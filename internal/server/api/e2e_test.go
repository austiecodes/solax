package api_test

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
	"github.com/austiecodes/solax/internal/core"
	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/internal/server/api"
	"github.com/austiecodes/solax/internal/store/sqlite"
	"github.com/austiecodes/solax/pkg/client"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

type nopDispatcher struct{}

func (nopDispatcher) Send(context.Context, core.ID, protocol.Envelope) (protocol.Ack, error) {
	return protocol.Ack{}, errs.ErrUnavailable
}
func (nopDispatcher) Online(core.ID) bool { return false }
func (nopDispatcher) Disconnect(core.ID)  {}

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
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "solax.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := auth.NewService(store, []byte("secret"))
	eng := core.NewEngine(core.Deps{Store: store, Dispatcher: nopDispatcher{}, Tokens: svc})
	srv := httptest.NewServer(api.NewRouter(api.Deps{
		Commands: eng, Enrollment: eng, Queries: eng, Auth: svc,
		Daemon: http.NotFoundHandler(), Stream: http.NotFoundHandler(),
	}))
	defer srv.Close()

	// Bootstrap, as "solax-server init" does.
	owner, err := eng.InitOwner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := svc.CreateClient(ctx, "", "test")
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
	cl, err := svc.Verify(ctx, machineToken)
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
	if _, err := svc.Verify(ctx, otherToken); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("pending machine verify = %v", err)
	}
	if _, err := c.ApproveMachine(ctx, enr.PairCode); err != nil {
		t.Fatal(err)
	}

	// The daemon of "mac" connects and reports its agents.
	if err := eng.MachineConnected(ctx, core.ID(reg.MachineID), protocol.Hello{Agents: []protocol.AgentInfo{
		{Name: "backend", Driver: ir.KindClaude}, {Name: "infra", Driver: ir.KindCodex}}}); err != nil {
		t.Fatal(err)
	}
	machines, err := c.Machines(ctx)
	if err != nil || len(machines) != 2 {
		t.Fatalf("machines = %+v, %v", machines, err)
	}
	for _, m := range machines {
		if m.ID == reg.MachineID && (len(m.Agents) != 2 || !m.Online) {
			t.Fatalf("mac = %+v", m)
		}
	}

	// Workspaces.
	w, err := c.CreateWorkspace(ctx, protocol.CreateWorkspaceRequest{Name: "api", Leader: "backend@mac"})
	if err != nil || w.Name != "api" || len(w.Members) != 1 || w.Members[0].Role != "leader" {
		t.Fatalf("create workspace = %+v, %v", w, err)
	}
	if _, err := c.CreateWorkspace(ctx, protocol.CreateWorkspaceRequest{Name: "api"}); status(t, err) != http.StatusConflict {
		t.Fatalf("duplicate workspace: %v", err)
	}
	if err := c.AddMember(ctx, "api", protocol.AddMemberRequest{Agent: "infra"}); err != nil {
		t.Fatal(err)
	}
	got, err := c.Workspace(ctx, "api")
	if err != nil || got.ID != w.ID || len(got.Members) != 2 {
		t.Fatalf("workspace = %+v, %v", got, err)
	}
	tasks, err := c.Tasks(ctx, "api")
	if err != nil || len(tasks) != 1 || !tasks[0].General {
		t.Fatalf("tasks = %+v, %v", tasks, err)
	}
	msgs, err := c.ReadMessages(ctx, "api", protocol.ReadMessagesQuery{})
	if err != nil || msgs == nil || len(msgs) != 0 {
		t.Fatalf("messages = %#v, %v", msgs, err)
	}
	obj, err := c.Recall(ctx, w.ID)
	if err != nil || obj.Type != "workspace" {
		t.Fatalf("recall = %+v, %v", obj, err)
	}

	// A worker agent token reads its workspace but cannot manage.
	agentToken, err := svc.IssueToken(ctx, core.TokenRequest{OwnerID: owner.ID, AgentID: core.ID(w.Leader),
		Workspace: core.ID(w.ID), Task: core.ID(tasks[0].ID)})
	if err != nil {
		t.Fatal(err)
	}
	worker := client.New(srv.URL, agentToken)
	if ws, err := worker.Workspaces(ctx); err != nil || len(ws) != 1 {
		t.Fatalf("worker workspaces = %+v, %v", ws, err)
	}
	if _, err := worker.CreateWorkspace(ctx, protocol.CreateWorkspaceRequest{Name: "evil"}); status(t, err) != http.StatusForbidden {
		t.Fatalf("worker create workspace: %v", err)
	}

	// Not built yet (A3): a clean 501.
	if _, err := c.CreateTask(ctx, "api", protocol.CreateTaskRequest{Brief: "x"}); !errors.Is(err, client.ErrNotImplemented) {
		t.Fatalf("create task = %v", err)
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
	if _, err := svc.Verify(ctx, machineToken); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("revoked machine verify = %v", err)
	}

	// Revoking the client token locks the client out.
	list, err := svc.Clients(ctx, "")
	if err != nil || len(list) != 1 || list[0].LastUsed.IsZero() {
		t.Fatalf("clients = %+v, %v", list, err)
	}
	if err := svc.RevokeClient(ctx, list[0].ID); err != nil {
		t.Fatal(err)
	}
	if code := status(t, c.Ping(ctx)); code != http.StatusUnauthorized {
		t.Fatalf("revoked client = %d", code)
	}
}
