package router_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/austiecodes/solax/internal/auth"
	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/internal/server/model"
	"github.com/austiecodes/solax/internal/server/router"
	"github.com/austiecodes/solax/internal/server/service"
	"github.com/austiecodes/solax/internal/server/stream"
	"github.com/austiecodes/solax/pkg/client"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

func status(t *testing.T, err error) int {
	t.Helper()
	var ce *client.Error
	if !errors.As(err, &ce) {
		t.Fatalf("want a server error, got %v", err)
	}
	return ce.Status
}

// TestTopologyOverHTTP walks phase A2 through the real router, auth and
// store with the Go client: bootstrap, machines, workspaces, scopes.
func TestTopologyOverHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	if err := model.Open(ctx, filepath.Join(t.TempDir(), "solax.db")); err != nil {
		t.Fatal(err)
	}
	defer model.Close()
	tokens := auth.NewTokens([]byte("secret"))
	// The service functions keep their defaults: no daemon is connected.
	srv := httptest.NewServer(router.New(tokens, http.NotFoundHandler(), stream.New()))
	defer srv.Close()

	// Bootstrap, as "solax server init" does.
	owner, err := service.InitOwner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := tokens.CreateClient(ctx, "", "test")
	if err != nil {
		t.Fatal(err)
	}
	c := client.New(srv.URL, token)
	if err := c.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if code := status(t, client.New(srv.URL, "sxc_wrong").Ping(ctx)); code != http.StatusUnauthorized {
		t.Fatalf("bad token = %d", code)
	}

	// A machine registers with a client token: approved at once.
	machineToken, _ := auth.NewToken(auth.PrefixMachine)
	reg, err := c.RegisterMachine(ctx, protocol.MachineEnrollment{Name: "mac", TokenHash: auth.Hash(machineToken)})
	if err != nil || reg.State != protocol.MachineApproved {
		t.Fatalf("register = %+v, %v", reg, err)
	}
	cl, err := tokens.Verify(ctx, machineToken)
	if err != nil || cl.Kind != auth.KindMachine || string(cl.Subject) != reg.MachineID {
		t.Fatalf("machine claims = %+v, %v", cl, err)
	}
	if code := status(t, client.New(srv.URL, machineToken).Ping(ctx)); code != http.StatusForbidden {
		t.Fatalf("machine token on REST = %d", code)
	}

	// Another machine enrolls without a token and is approved by code.
	otherToken, _ := auth.NewToken(auth.PrefixMachine)
	enr, err := client.New(srv.URL, "").EnrollMachine(ctx, protocol.MachineEnrollment{Name: "pi", TokenHash: auth.Hash(otherToken)})
	if err != nil || enr.State != protocol.MachinePending || enr.PairCode == "" {
		t.Fatalf("enroll = %+v, %v", enr, err)
	}
	if _, err := tokens.Verify(ctx, otherToken); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("pending machine verify = %v", err)
	}
	if _, err := c.ApproveMachine(ctx, enr.PairCode); err != nil {
		t.Fatal(err)
	}

	// The daemon of "mac" connects and reports its agent CLIs; a person
	// sets up the agents.
	if err := service.MachineConnected(ctx, model.ID(reg.MachineID), protocol.Hello{Runtimes: []protocol.RuntimeProbe{
		{Kind: ir.KindClaude, OK: true}, {Kind: ir.KindCodex, OK: true}}}); err != nil {
		t.Fatal(err)
	}
	for _, a := range []protocol.CreateAgentRequest{
		{Machine: "mac", Name: "backend", Runtime: ir.KindClaude, Dir: "/api", Description: "Go backend"},
		{Machine: reg.MachineID, Name: "infra", Runtime: ir.KindCodex, Dir: "/infra"},
	} {
		if _, err := c.CreateAgent(ctx, a); err != nil {
			t.Fatalf("create agent %s: %v", a.Name, err)
		}
	}
	machines, err := c.Machines(ctx)
	if err != nil || len(machines) != 2 {
		t.Fatalf("machines = %+v, %v", machines, err)
	}
	agents, err := c.Agents(ctx)
	if err != nil || len(agents) != 2 || !agents[0].Online || agents[0].Machine != "mac" {
		t.Fatalf("agents = %+v, %v", agents, err)
	}

	// Recall follows IDs.
	obj, err := c.Recall(ctx, agents[0].ID)
	if err != nil || obj.Type != "agent" || obj.Links["machine"] != reg.MachineID {
		t.Fatalf("recall = %+v, %v", obj, err)
	}

	// A worker agent token reads, but cannot dispatch or manage.
	agentToken, err := tokens.IssueToken(ctx, model.TokenRequest{OwnerID: owner.ID, AgentID: model.ID(agents[0].ID), Job: "job_1"})
	if err != nil {
		t.Fatal(err)
	}
	worker := client.New(srv.URL, agentToken)
	if ags, err := worker.Agents(ctx); err != nil || len(ags) != 2 {
		t.Fatalf("worker agents = %+v, %v", ags, err)
	}
	if _, err := worker.Dispatch(ctx, "infra", "x"); status(t, err) != http.StatusForbidden {
		t.Fatalf("worker dispatch: %v", err)
	}
	if _, err := worker.Jobs(ctx, 0); status(t, err) != http.StatusForbidden {
		t.Fatalf("worker jobs: %v", err)
	}

	// A bad body is a 400, not a 500.
	res, err := http.Post(srv.URL+"/v1/machines/enroll", "application/json", strings.NewReader("{"))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad body = %d", res.StatusCode)
	}

	// Revocation ends the machine token.
	if err := c.RevokeMachine(ctx, reg.MachineID); err != nil {
		t.Fatal(err)
	}
	if _, err := tokens.Verify(ctx, machineToken); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("revoked machine verify = %v", err)
	}

	// Revoking the client token locks the client out.
	list, err := tokens.Clients(ctx, "")
	if err != nil || len(list) != 1 || list[0].LastUsed.IsZero() {
		t.Fatalf("clients = %+v, %v", list, err)
	}
	if err := tokens.RevokeClient(ctx, list[0].ID); err != nil {
		t.Fatal(err)
	}
	if code := status(t, c.Ping(ctx)); code != http.StatusUnauthorized {
		t.Fatalf("revoked client = %d", code)
	}
}
