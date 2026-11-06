const API_BASE = (import.meta.env.VITE_API_URL ?? "/api").replace(/\/$/, "")

export class ApiError extends Error {
  status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = "ApiError"
    this.status = status
  }
}

type RequestOptions = {
  method?: string
  body?: unknown
  token?: string
}

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = {}
  if (options.token) {
    headers.Authorization = `Bearer ${options.token}`
  }
  if (options.body !== undefined) {
    headers["Content-Type"] = "application/json"
  }

  const response = await fetch(`${API_BASE}${path}`, {
    method: options.method ?? "GET",
    headers,
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
  })

  if (response.status === 204) {
    return undefined as T
  }

  const text = await response.text()
  let data: unknown = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = null
    }
  }

  if (!response.ok) {
    const message =
      data && typeof data === "object" && "error" in data
        ? String((data as { error: unknown }).error)
        : `Request failed with status ${response.status}`
    throw new ApiError(response.status, message)
  }

  return data as T
}

export type User = {
  id: string
  email: string
  created_at: string
}

export type Project = {
  id: string
  user_id: string
  name: string
  created_at: string
  updated_at: string
  default_queue_name: string
  default_max_attempts: number
  default_priority: number
  job_retention_days: number
  max_concurrency: number
  rate_limit_per_minute: number
  default_job_timeout_seconds: number
}

export type ProjectUpdate = {
  name?: string
  default_queue_name?: string
  default_max_attempts?: number
  default_priority?: number
  job_retention_days?: number
  max_concurrency?: number
  rate_limit_per_minute?: number
  default_job_timeout_seconds?: number
}

export type ApiKey = {
  id: string
  project_id: string
  name: string
  key_prefix: string
  revoked_at: string | null
  expires_at: string | null
  last_used_at: string | null
  created_at: string
}

export type WorkerToken = {
  id: string
  project_id: string
  name: string
  token_prefix: string
  revoked_at: string | null
  expires_at: string | null
  last_used_at: string | null
  rotated_at: string | null
  replaced_by_id: string | null
  created_at: string
}

export type WorkerPool = {
  id: string
  project_id: string
  name: string
  concurrency: number
  status: "active" | "paused"
  queue_names: string[]
  job_timeout_seconds: number
  max_attempts: number
  retry_backoff: "fixed" | "exponential"
  retry_base_delay_seconds: number
  retry_max_delay_seconds: number
  allowed_cidrs: string[]
  created_at: string
  updated_at: string
}

export type WorkerPoolStat = {
  pool_id: string
  online_workers: number
  offline_workers: number
  running_jobs: number
  queue_depth: number
  jobs_per_minute: number
  success_rate: number
  avg_duration_seconds: number
}

export type WorkerSummary = {
  id: string
  project_id: string
  worker_pool_id: string | null
  name: string
  status: "online" | "offline"
  mode: "direct" | "remote"
  last_heartbeat_at: string | null
  created_at: string
  running_jobs: number
}

export type WorkerPoolInput = {
  name?: string
  concurrency?: number
  status?: "active" | "paused"
  queue_names?: string[]
  job_timeout_seconds?: number
  max_attempts?: number
  retry_backoff?: "fixed" | "exponential"
  retry_base_delay_seconds?: number
  retry_max_delay_seconds?: number
  allowed_cidrs?: string[]
}

export type JobStatus =
  | "pending"
  | "scheduled"
  | "running"
  | "succeeded"
  | "failed"
  | "cancelled"

export type Job = {
  id: string
  project_id: string
  queue_name: string
  job_type: string
  payload: unknown
  status: JobStatus
  priority: number
  attempts: number
  max_attempts: number
  scheduled_at: string
  created_at: string
  updated_at: string
}

export type Execution = {
  id: string
  job_id: string
  worker_id: string | null
  attempt_number: number
  status: "running" | "succeeded" | "failed" | "cancelled"
  started_at: string
  finished_at: string | null
  error_message: string | null
  result: unknown
}

export type LoginResult = {
  token: string
  token_type: string
  expires_at: string
  user: User
}

export const api = {
  register(email: string, password: string) {
    return request<{ user: User }>("/v1/auth/register", {
      method: "POST",
      body: { email, password },
    })
  },

  login(email: string, password: string) {
    return request<LoginResult>("/v1/auth/login", {
      method: "POST",
      body: { email, password },
    })
  },

  me(token: string) {
    return request<{ user: User }>("/v1/auth/me", { token })
  },

  logout(token: string) {
    return request<void>("/v1/auth/logout", { method: "POST", token })
  },

  listProjects(token: string) {
    return request<{ projects: Project[] }>("/v1/projects", { token })
  },

  getProject(token: string, projectId: string) {
    return request<{ project: Project }>(`/v1/projects/${projectId}`, { token })
  },

  createProject(token: string, name: string) {
    return request<{ project: Project }>("/v1/projects", {
      method: "POST",
      token,
      body: { name },
    })
  },

  updateProject(token: string, projectId: string, input: ProjectUpdate) {
    return request<{ project: Project }>(`/v1/projects/${projectId}`, {
      method: "PATCH",
      token,
      body: input,
    })
  },

  deleteProject(token: string, projectId: string) {
    return request<void>(`/v1/projects/${projectId}`, {
      method: "DELETE",
      token,
    })
  },

  listApiKeys(token: string, projectId: string) {
    return request<{ api_keys: ApiKey[] }>(
      `/v1/projects/${projectId}/api-keys`,
      { token },
    )
  },

  createApiKey(
    token: string,
    projectId: string,
    name: string,
    expiresInDays?: number,
  ) {
    return request<{ api_key: ApiKey; key: string }>(
      `/v1/projects/${projectId}/api-keys`,
      {
        method: "POST",
        token,
        body: { name, expires_in_days: expiresInDays },
      },
    )
  },

  revokeApiKey(token: string, projectId: string, keyId: string) {
    return request<void>(`/v1/projects/${projectId}/api-keys/${keyId}`, {
      method: "DELETE",
      token,
    })
  },

  listWorkerTokens(token: string, projectId: string) {
    return request<{ worker_tokens: WorkerToken[] }>(
      `/v1/projects/${projectId}/worker-tokens`,
      { token },
    )
  },

  createWorkerToken(
    token: string,
    projectId: string,
    name: string,
    expiresInDays?: number,
  ) {
    return request<{ worker_token: WorkerToken; token: string }>(
      `/v1/projects/${projectId}/worker-tokens`,
      {
        method: "POST",
        token,
        body: { name, expires_in_days: expiresInDays },
      },
    )
  },

  revokeWorkerToken(token: string, projectId: string, tokenId: string) {
    return request<void>(
      `/v1/projects/${projectId}/worker-tokens/${tokenId}`,
      { method: "DELETE", token },
    )
  },

  listWorkerPools(token: string, projectId: string) {
    return request<{ worker_pools: WorkerPool[] }>(
      `/v1/projects/${projectId}/worker-pools`,
      { token },
    )
  },

  createWorkerPool(
    token: string,
    projectId: string,
    input: WorkerPoolInput & { name: string },
  ) {
    return request<{ worker_pool: WorkerPool }>(
      `/v1/projects/${projectId}/worker-pools`,
      {
        method: "POST",
        token,
        body: input,
      },
    )
  },

  updateWorkerPool(
    token: string,
    projectId: string,
    poolId: string,
    input: WorkerPoolInput,
  ) {
    return request<{ worker_pool: WorkerPool }>(
      `/v1/projects/${projectId}/worker-pools/${poolId}`,
      { method: "PATCH", token, body: input },
    )
  },

  workerPoolStats(token: string, projectId: string) {
    return request<{ stats: WorkerPoolStat[] }>(
      `/v1/projects/${projectId}/worker-pools/stats`,
      { token },
    )
  },

  listWorkers(token: string, projectId: string, poolId?: string) {
    const query = poolId ? `?pool_id=${poolId}` : ""
    return request<{ workers: WorkerSummary[] }>(
      `/v1/projects/${projectId}/workers${query}`,
      { token },
    )
  },

  rotateWorkerToken(token: string, projectId: string, tokenId: string) {
    return request<{ worker_token: WorkerToken; token: string; grace_seconds: number }>(
      `/v1/projects/${projectId}/worker-tokens/${tokenId}/rotate`,
      { method: "POST", token },
    )
  },

  deleteWorkerPool(token: string, projectId: string, poolId: string) {
    return request<void>(
      `/v1/projects/${projectId}/worker-pools/${poolId}`,
      { method: "DELETE", token },
    )
  },

  createEnrollmentToken(
    token: string,
    projectId: string,
    poolId: string,
    name?: string,
    expiresInMinutes?: number,
  ) {
    return request<{ enrollment_token: WorkerToken; token: string }>(
      `/v1/projects/${projectId}/worker-pools/${poolId}/enrollment-tokens`,
      {
        method: "POST",
        token,
        body: { name, expires_in_minutes: expiresInMinutes },
      },
    )
  },

  listJobs(token: string, projectId: string, limit = 50) {
    return request<{ jobs: Job[] }>(
      `/v1/projects/${projectId}/jobs?limit=${limit}`,
      { token },
    )
  },

  getJob(token: string, projectId: string, jobId: string) {
    return request<{ job: Job }>(
      `/v1/projects/${projectId}/jobs/${jobId}`,
      { token },
    )
  },

  submitJob(
    token: string,
    projectId: string,
    input: {
      queue_name: string
      job_type: string
      payload: unknown
      priority: number
      max_attempts: number
      scheduled_at?: string
    },
  ) {
    return request<{ job: Job }>(`/v1/projects/${projectId}/jobs`, {
      method: "POST",
      token,
      body: input,
    })
  },

  listExecutions(token: string, projectId: string, jobId: string) {
    return request<{ executions: Execution[] }>(
      `/v1/projects/${projectId}/jobs/${jobId}/executions`,
      { token },
    )
  },
}
