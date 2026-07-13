import { appPath } from "./base";
import { protectedAPIReady, protectedJSON } from "./api";

// Agent status service

export type AgentStatus = {
  name: string;
  protocol?: string;
  brief?: string;
  installed: boolean;
  available: boolean;
  version?: string;
  error?: string;
  last_probe?: string;
  current_model_id?: string;
  current_mode_id?: string;
  default_model_id?: string;
  default_effort?: string;
  default_fast_service?: string;
  last_config_selection?: AgentLastConfigSelection;
  supports_api_provider_switch?: boolean;
  supported_api_provider_protocols?: string[];
  supports_fast_service?: boolean;
  efforts?: string[];
  models?: AgentModelInfo[];
  modes?: AgentModeInfo[];
  models_error?: string;
  modes_error?: string;
  commands?: AgentCommandInfo[];
  commands_error?: string;
  capabilities?: string[];
  supports_docker_backup?: boolean;
  supports_online_update?: boolean;
  install_commands?: string[];
  update_commands?: string[];
  remote_server_id?: string;
  remote_server_name?: string;
  remote_agent?: string;
  remote_shell?: string;
};

export type AgentLastConfigSelection = {
  type?: string;
  id?: string;
  name?: string;
};

export type AgentModelInfo = {
  id: string;
  name: string;
  description?: string;
  hidden?: boolean;
  supportEffort?: boolean;
  efforts?: string[];
  default_effort?: string;
};

export type AgentModeInfo = {
  id: string;
  name: string;
  description?: string;
};

export type AgentCommandInfo = {
  name: string;
  description?: string;
  argument_hint?: string;
};

export type ShellStatus = {
  id: string;
  name?: string;
  label: string;
  command: string;
  resolved_command?: string;
  args?: string[];
  default?: boolean;
  remote_server_id?: string;
  remote_server_name?: string;
  remote_shell?: string;
};

function normalizeEfforts(input: unknown): string[] | undefined {
  if (!Array.isArray(input)) {
    return undefined;
  }
  const seen = new Set<string>();
  const efforts: string[] = [];
  for (const item of input) {
    if (typeof item !== "string") {
      continue;
    }
    const value = item.trim().toLowerCase();
    // 思考等级由 Agent 的模型目录定义，不能用前端白名单丢弃 ultra 等新增能力。
    if (!value) {
      continue;
    }
    if (seen.has(value)) {
      continue;
    }
    seen.add(value);
    efforts.push(value);
  }
  return efforts;
}

function normalizeAgentStatus(input: unknown): AgentStatus | null {
  if (!input || typeof input !== "object") {
    return null;
  }
  const agent = input as AgentStatus;
  const models = Array.isArray(agent.models)
    ? agent.models.map((model) => ({
        ...model,
        efforts: normalizeEfforts(model.efforts),
        default_effort:
          typeof model.default_effort === "string"
            ? model.default_effort.trim().toLowerCase()
            : "",
      }))
    : agent.models;
  return {
    ...agent,
    efforts: normalizeEfforts(agent.efforts),
    models,
    default_fast_service:
      typeof agent.default_fast_service === "string"
        ? agent.default_fast_service
        : "",
    supports_fast_service: !!agent.supports_fast_service,
    capabilities: Array.isArray(agent.capabilities) ? agent.capabilities.map((item) => String(item)) : undefined,
    supports_docker_backup: !!agent.supports_docker_backup,
    supports_online_update: !!agent.supports_online_update,
  };
}

let cachedAgents: AgentStatus[] = [];
let cachedAgentCatalog: AgentStatus[] = [];
let cachedShells: ShellStatus[] = [];
let lastFetch = 0;
let lastCatalogFetch = 0;
let inFlightAgents: Promise<{ agents: AgentStatus[]; shells: ShellStatus[] }> | null = null;
let inFlightCatalog: Promise<{ agents: AgentStatus[]; shells: ShellStatus[] }> | null = null;
const CACHE_TTL = 30000; // 30 seconds

function normalizeShellStatus(input: unknown): ShellStatus | null {
  if (!input || typeof input !== "object") {
    return null;
  }
  const shell = input as ShellStatus;
  const id = String(shell.id || shell.command || "").trim();
  const command = String(shell.command || id).trim();
  if (!id || !command) {
    return null;
  }
  return {
    id,
    command,
    name: typeof shell.name === "string" ? shell.name : undefined,
    resolved_command: typeof shell.resolved_command === "string" ? shell.resolved_command : undefined,
    label: String(shell.name || shell.label || id).trim() || id,
    args: Array.isArray(shell.args) ? shell.args.map((item) => String(item)) : undefined,
    default: !!shell.default,
    remote_server_id: typeof shell.remote_server_id === "string" ? shell.remote_server_id : undefined,
    remote_server_name: typeof shell.remote_server_name === "string" ? shell.remote_server_name : undefined,
    remote_shell: typeof shell.remote_shell === "string" ? shell.remote_shell : undefined,
  };
}

async function fetchAgentRuntime(force = false, includeAll = false): Promise<{ agents: AgentStatus[]; shells: ShellStatus[] }> {
  const now = Date.now();
  const agentCache = includeAll ? cachedAgentCatalog : cachedAgents;
  const agentLastFetch = includeAll ? lastCatalogFetch : lastFetch;
  const inFlight = includeAll ? inFlightCatalog : inFlightAgents;
  if (!force && agentCache.length > 0 && now - agentLastFetch < CACHE_TTL) {
    if (includeAll) {
      return { agents: cachedAgentCatalog, shells: cachedShells };
    }
    return { agents: cachedAgents, shells: cachedShells };
  }
  if (inFlight) {
    return inFlight;
  }
  if (!protectedAPIReady()) {
    return { agents: agentCache, shells: cachedShells };
  }

  const request = (async () => {
    const data = await protectedJSON<any>(appPath(includeAll ? "/api/agents?all=1" : "/api/agents"));
    const agentItems: unknown[] = Array.isArray(data) ? data : Array.isArray(data?.agents) ? data.agents : [];
    const shellItems: unknown[] = Array.isArray(data?.shells) ? data.shells : [];
    const nextAgents = agentItems
      ? agentItems.map(normalizeAgentStatus).filter((item): item is AgentStatus => item !== null)
      : [];
    if (includeAll) {
      cachedAgentCatalog = nextAgents;
      lastCatalogFetch = now;
    } else {
      cachedAgents = nextAgents;
      lastFetch = now;
    }
    cachedShells = shellItems.map(normalizeShellStatus).filter((item): item is ShellStatus => item !== null);
    return { agents: nextAgents, shells: cachedShells };
  })();
  if (includeAll) {
    inFlightCatalog = request;
  } else {
    inFlightAgents = request;
  }
  try {
    return await request;
  } catch (err) {
    console.error("Failed to fetch agents:", err);
    return { agents: agentCache, shells: cachedShells };
  } finally {
    if (includeAll) {
      inFlightCatalog = null;
    } else {
      inFlightAgents = null;
    }
  }
}

export async function fetchAgents(force = false): Promise<AgentStatus[]> {
  const data = await fetchAgentRuntime(force);
  return data.agents;
}

export async function fetchAgentCatalog(force = false): Promise<AgentStatus[]> {
  const data = await fetchAgentRuntime(force, true);
  return data.agents;
}

export async function restartAgent(agent: string): Promise<{ restarting: boolean; agent: string }> {
  return protectedJSON<{ restarting: boolean; agent: string }>(appPath("/api/agents/restart"), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ agent }),
  });
}

export type AgentLifecycleAction = "install" | "update";

export type AgentLifecycleResult = {
  agent: string;
  action: AgentLifecycleAction;
  success: boolean;
  exit_code: number;
  output?: string;
  error?: string;
  interrupted?: boolean;
};

export async function runAgentLifecycle(agent: string, action: AgentLifecycleAction): Promise<AgentLifecycleResult> {
  const result = await protectedJSON<AgentLifecycleResult>(appPath("/api/agents/lifecycle"), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ agent, action }),
  });
  cachedAgents = [];
  cachedAgentCatalog = [];
  lastFetch = 0;
  lastCatalogFetch = 0;
  return result;
}

export async function fetchShells(force = false): Promise<ShellStatus[]> {
  const data = await fetchAgentRuntime(force);
  return data.shells;
}
