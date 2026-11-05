// The SolaxApi implementation for the desktop app. Each call goes to the Go
// side through MyGo bindings. The Go side holds the token.

import { Machines, Profiles, Super, Tasks, Workspaces, events } from "../mygo";
import type { SolaxApi } from "./api";

export function desktopApi(): SolaxApi {
  return {
    profiles: Profiles,
    super: Super,
    workspaces: Workspaces,
    tasks: Tasks,
    machines: Machines,
    on: {
      connection: (fn) => events.connection.on(fn),
      messageCreated: (fn) => events.messageCreated.on(fn),
      taskUpdated: (fn) => events.taskUpdated.on(fn),
      machineUpdated: (fn) => events.machineUpdated.on(fn),
      superReplied: (fn) => events.superReplied.on(fn),
    },
  };
}
