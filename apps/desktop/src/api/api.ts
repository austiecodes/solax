// SolaxApi is the only way the views reach Solax. The desktop app
// implements it with MyGo bindings (desktop.ts). A future web console can
// implement it with fetch on the REST API. The views do not change.

import type {
  Connection,
  Machine,
  Message,
  PostMessageRequest,
  Profile,
  Task,
  Workspace,
} from "../mygo";

export type { Connection, Machine, Message, PostMessageRequest, Profile, Task, Workspace };

/** Unsubscribe removes an event listener. */
export type Unsubscribe = () => void;

export interface SolaxApi {
  profiles: {
    list(): Promise<Profile[]>;
    current(): Promise<Profile>;
    save(p: Profile, token: string): Promise<void>;
    use(name: string): Promise<void>;
    remove(name: string): Promise<void>;
    test(): Promise<void>;
  };
  super: {
    history(since: number): Promise<Message[]>;
    send(text: string): Promise<Message>;
  };
  workspaces: {
    list(): Promise<Workspace[]>;
    get(ws: string): Promise<Workspace>;
    messages(ws: string, task: string, since: number): Promise<Message[]>;
    post(ws: string, req: PostMessageRequest): Promise<Message>;
  };
  tasks: {
    get(task: string): Promise<Task>;
    cancel(task: string): Promise<void>;
    summarize(task: string): Promise<void>;
  };
  machines: {
    list(): Promise<Machine[]>;
    approve(code: string): Promise<Machine>;
    revoke(machineID: string): Promise<void>;
  };
  on: {
    connection(fn: (c: Connection) => void): Unsubscribe;
    messageCreated(fn: (m: Message) => void): Unsubscribe;
    taskUpdated(fn: (t: Task) => void): Unsubscribe;
    machineUpdated(fn: (m: Machine) => void): Unsubscribe;
    superReplied(fn: (m: Message) => void): Unsubscribe;
  };
}
