import { useState, type FormEvent } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  Copy,
  KeyRound,
  Layers,
  Loader2,
  Pause,
  Pencil,
  Play,
  Plus,
  Trash2,
} from "lucide-react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import {
  api,
  ApiError,
  type WorkerPool,
  type WorkerPoolInput,
  type WorkerPoolStat,
} from "@/lib/api"
import { useAuth } from "@/lib/auth"
import { formatDate } from "@/lib/format"

type PoolForm = {
  name: string
  concurrency: string
  queues: string
  jobTimeout: string
  maxAttempts: string
  retryBackoff: "fixed" | "exponential"
  retryBase: string
  retryMax: string
  allowedCidrs: string
}

const emptyForm: PoolForm = {
  name: "",
  concurrency: "4",
  queues: "",
  jobTimeout: "",
  maxAttempts: "",
  retryBackoff: "exponential",
  retryBase: "",
  retryMax: "",
  allowedCidrs: "",
}

function formFromPool(pool: WorkerPool): PoolForm {
  return {
    name: pool.name,
    concurrency: String(pool.concurrency),
    queues: pool.queue_names.join(", "),
    jobTimeout: pool.job_timeout_seconds ? String(pool.job_timeout_seconds) : "",
    maxAttempts: pool.max_attempts ? String(pool.max_attempts) : "",
    retryBackoff: pool.retry_backoff,
    retryBase: pool.retry_base_delay_seconds
      ? String(pool.retry_base_delay_seconds)
      : "",
    retryMax: pool.retry_max_delay_seconds
      ? String(pool.retry_max_delay_seconds)
      : "",
    allowedCidrs: pool.allowed_cidrs.join(", "),
  }
}

function parseQueues(value: string): string[] {
  return value
    .split(",")
    .map((queue) => queue.trim())
    .filter(Boolean)
}

function parseOptionalNumber(value: string): number {
  const trimmed = value.trim()
  if (!trimmed) return 0
  return Number(trimmed)
}

function toInput(form: PoolForm): WorkerPoolInput {
  return {
    name: form.name.trim(),
    concurrency: parseOptionalNumber(form.concurrency),
    queue_names: parseQueues(form.queues),
    job_timeout_seconds: parseOptionalNumber(form.jobTimeout),
    max_attempts: parseOptionalNumber(form.maxAttempts),
    retry_backoff: form.retryBackoff,
    retry_base_delay_seconds: parseOptionalNumber(form.retryBase),
    retry_max_delay_seconds: parseOptionalNumber(form.retryMax),
    allowed_cidrs: parseQueues(form.allowedCidrs),
  }
}

function validNumbers(input: WorkerPoolInput): string | null {
  if (!input.name) return "Pool name is required"
  if (!Number.isInteger(input.concurrency) || (input.concurrency ?? 0) < 1 || (input.concurrency ?? 0) > 1000) {
    return "Concurrency must be between 1 and 1000"
  }
  for (const [label, value, max] of [
    ["Job timeout", input.job_timeout_seconds, 86400],
    ["Max attempts", input.max_attempts, 100],
    ["Retry base delay", input.retry_base_delay_seconds, 86400],
    ["Retry max delay", input.retry_max_delay_seconds, 86400],
  ] as const) {
    if (!Number.isInteger(value) || (value ?? 0) < 0 || (value ?? 0) > max) {
      return `${label} is out of range`
    }
  }
  if ((input.allowed_cidrs ?? []).length > 50) {
    return "At most 50 allowed CIDRs"
  }
  if (
    (input.retry_base_delay_seconds ?? 0) > 0 &&
    (input.retry_max_delay_seconds ?? 0) > 0 &&
    (input.retry_max_delay_seconds ?? 0) < (input.retry_base_delay_seconds ?? 0)
  ) {
    return "Retry max delay must be at least the base delay"
  }
  return null
}

export function WorkerPoolPanel({ projectId }: { projectId: string }) {
  const { token } = useAuth()
  const queryClient = useQueryClient()
  const [form, setForm] = useState<PoolForm>(emptyForm)
  const [editing, setEditing] = useState<WorkerPool | null>(null)
  const [editForm, setEditForm] = useState<PoolForm>(emptyForm)
  const [enrollmentToken, setEnrollmentToken] = useState("")

  const poolsQuery = useQuery({
    queryKey: ["worker-pools", projectId],
    queryFn: () => api.listWorkerPools(token!, projectId),
    enabled: Boolean(token),
  })

  const statsQuery = useQuery({
    queryKey: ["worker-pool-stats", projectId],
    queryFn: () => api.workerPoolStats(token!, projectId),
    enabled: Boolean(token),
    refetchInterval: 5000,
  })

  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: ["worker-pools", projectId] })
    queryClient.invalidateQueries({ queryKey: ["worker-pool-stats", projectId] })
  }

  const createPool = useMutation({
    mutationFn: (input: WorkerPoolInput & { name: string }) =>
      api.createWorkerPool(token!, projectId, input),
    onSuccess: () => {
      setForm(emptyForm)
      refresh()
      toast.success("Worker pool created")
    },
    onError: (error) => {
      toast.error(error instanceof ApiError ? error.message : "Could not create worker pool")
    },
  })

  const updatePool = useMutation({
    mutationFn: (input: { poolId: string; values: WorkerPoolInput }) =>
      api.updateWorkerPool(token!, projectId, input.poolId, input.values),
    onSuccess: () => {
      setEditing(null)
      refresh()
      toast.success("Worker pool updated")
    },
    onError: (error) => {
      toast.error(error instanceof ApiError ? error.message : "Could not update worker pool")
    },
  })

  const deletePool = useMutation({
    mutationFn: (poolId: string) => api.deleteWorkerPool(token!, projectId, poolId),
    onSuccess: () => {
      refresh()
      toast.success("Worker pool deleted")
    },
    onError: (error) => {
      toast.error(error instanceof ApiError ? error.message : "Could not delete worker pool")
    },
  })

  const issueEnrollment = useMutation({
    mutationFn: (poolId: string) => api.createEnrollmentToken(token!, projectId, poolId),
    onSuccess: (result) => {
      setEnrollmentToken(result.token)
      toast.success("Enrollment token created")
    },
    onError: (error) => {
      toast.error(error instanceof ApiError ? error.message : "Could not create enrollment token")
    },
  })

  function handleCreate(event: FormEvent) {
    event.preventDefault()
    const input = toInput(form) as WorkerPoolInput & { name: string }
    const problem = validNumbers(input)
    if (problem) {
      toast.error(problem)
      return
    }
    createPool.mutate(input)
  }

  function handleEdit(event: FormEvent) {
    event.preventDefault()
    if (!editing) return
    const input = toInput(editForm)
    const problem = validNumbers(input)
    if (problem) {
      toast.error(problem)
      return
    }
    updatePool.mutate({ poolId: editing.id, values: input })
  }

  async function copyToken() {
    await navigator.clipboard.writeText(enrollmentToken)
    toast.success("Enrollment token copied")
  }

  const pools = poolsQuery.data?.worker_pools ?? []
  const stats = new Map(
    (statsQuery.data?.stats ?? []).map((item) => [item.pool_id, item]),
  )

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Layers className="size-4" />
          Worker pools
        </CardTitle>
        <CardDescription>
          Desired worker configuration. The supervisor launches one worker per
          active pool and stops it when the pool is paused.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        {enrollmentToken && (
          <div className="rounded-lg border border-emerald-500/30 bg-emerald-500/10 p-3">
            <p className="text-xs font-medium text-emerald-700 dark:text-emerald-300">
              Copy this enrollment token now. It is single-use, expires in 15
              minutes, and will not be shown again.
            </p>
            <div className="mt-2 flex items-center gap-2">
              <code className="min-w-0 flex-1 truncate rounded bg-background/60 px-2 py-1.5 font-mono text-[11px]">
                {enrollmentToken}
              </code>
              <Button type="button" size="icon-sm" variant="outline" onClick={copyToken}>
                <Copy className="size-3.5" />
              </Button>
            </div>
          </div>
        )}

        <form className="space-y-3" onSubmit={handleCreate}>
          <div className="grid gap-3 sm:grid-cols-[1fr_150px]">
            <div className="space-y-2">
              <Label htmlFor="worker-pool-name">New pool name</Label>
              <Input
                id="worker-pool-name"
                placeholder="General workers"
                value={form.name}
                onChange={(event) => setForm({ ...form, name: event.target.value })}
                maxLength={100}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="worker-pool-concurrency">Concurrency</Label>
              <Input
                id="worker-pool-concurrency"
                type="number"
                min={1}
                max={1000}
                value={form.concurrency}
                onChange={(event) => setForm({ ...form, concurrency: event.target.value })}
              />
            </div>
          </div>

          <div className="space-y-2">
            <Label htmlFor="worker-pool-queues">Queues</Label>
            <Input
              id="worker-pool-queues"
              placeholder="default, emails (blank = all queues)"
              value={form.queues}
              onChange={(event) => setForm({ ...form, queues: event.target.value })}
            />
          </div>

          <div className="grid gap-3 sm:grid-cols-3">
            <div className="space-y-2">
              <Label htmlFor="worker-pool-timeout">Job timeout (s)</Label>
              <Input
                id="worker-pool-timeout"
                type="number"
                min={0}
                max={86400}
                placeholder="Project default"
                value={form.jobTimeout}
                onChange={(event) => setForm({ ...form, jobTimeout: event.target.value })}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="worker-pool-attempts">Max attempts</Label>
              <Input
                id="worker-pool-attempts"
                type="number"
                min={0}
                max={100}
                placeholder="Job default"
                value={form.maxAttempts}
                onChange={(event) => setForm({ ...form, maxAttempts: event.target.value })}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="worker-pool-backoff">Retry backoff</Label>
              <select
                id="worker-pool-backoff"
                className="h-8 w-full rounded-lg border border-input bg-transparent px-2.5 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30"
                value={form.retryBackoff}
                onChange={(event) =>
                  setForm({
                    ...form,
                    retryBackoff: event.target.value as PoolForm["retryBackoff"],
                  })
                }
              >
                <option value="exponential">Exponential</option>
                <option value="fixed">Fixed</option>
              </select>
            </div>
          </div>

          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="worker-pool-retry-base">Retry base delay (s)</Label>
              <Input
                id="worker-pool-retry-base"
                type="number"
                min={0}
                max={86400}
                placeholder="Worker default"
                value={form.retryBase}
                onChange={(event) => setForm({ ...form, retryBase: event.target.value })}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="worker-pool-retry-max">Retry max delay (s)</Label>
              <Input
                id="worker-pool-retry-max"
                type="number"
                min={0}
                max={86400}
                placeholder="Worker default"
                value={form.retryMax}
                onChange={(event) => setForm({ ...form, retryMax: event.target.value })}
              />
            </div>
          </div>

          <div className="space-y-2">
            <Label htmlFor="worker-pool-cidrs">Allowed CIDRs</Label>
            <Input
              id="worker-pool-cidrs"
              placeholder="10.0.0.0/8, 203.0.113.7/32 (blank = any address)"
              value={form.allowedCidrs}
              onChange={(event) => setForm({ ...form, allowedCidrs: event.target.value })}
            />
          </div>

          <Button type="submit" className="w-full" disabled={createPool.isPending}>
            {createPool.isPending ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <Plus className="size-4" />
            )}
            Create worker pool
          </Button>
        </form>

        {poolsQuery.isLoading ? (
          <div className="space-y-2">
            <Skeleton className="h-14 rounded-lg" />
            <Skeleton className="h-14 rounded-lg" />
          </div>
        ) : pools.length === 0 ? (
          <p className="py-2 text-sm text-muted-foreground">
            No worker pools yet. Create one to have the supervisor run workers
            for this project.
          </p>
        ) : (
          <div className="divide-y rounded-lg border">
            {pools.map((pool) => (
              <WorkerPoolRow
                key={pool.id}
                pool={pool}
                stat={stats.get(pool.id)}
                busy={
                  updatePool.isPending && updatePool.variables?.poolId === pool.id
                }
                issuing={
                  issueEnrollment.isPending && issueEnrollment.variables === pool.id
                }
                onEdit={() => {
                  setEditing(pool)
                  setEditForm(formFromPool(pool))
                }}
                onToggle={() =>
                  updatePool.mutate({
                    poolId: pool.id,
                    values: {
                      status: pool.status === "active" ? "paused" : "active",
                    },
                  })
                }
                onEnroll={() => issueEnrollment.mutate(pool.id)}
                onDelete={() => deletePool.mutate(pool.id)}
              />
            ))}
          </div>
        )}
      </CardContent>

      <Dialog open={editing !== null} onOpenChange={(open) => !open && setEditing(null)}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>Edit worker pool</DialogTitle>
            <DialogDescription>
              Changes restart the pool's worker on the next supervisor tick.
            </DialogDescription>
          </DialogHeader>
          <form className="space-y-3" onSubmit={handleEdit}>
            <div className="grid gap-3 sm:grid-cols-[1fr_130px]">
              <div className="space-y-2">
                <Label htmlFor="edit-pool-name">Name</Label>
                <Input
                  id="edit-pool-name"
                  value={editForm.name}
                  onChange={(event) =>
                    setEditForm({ ...editForm, name: event.target.value })
                  }
                  maxLength={100}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="edit-pool-concurrency">Concurrency</Label>
                <Input
                  id="edit-pool-concurrency"
                  type="number"
                  min={1}
                  max={1000}
                  value={editForm.concurrency}
                  onChange={(event) =>
                    setEditForm({ ...editForm, concurrency: event.target.value })
                  }
                />
              </div>
            </div>
            <div className="space-y-2">
              <Label htmlFor="edit-pool-queues">Queues</Label>
              <Input
                id="edit-pool-queues"
                placeholder="default, emails (blank = all queues)"
                value={editForm.queues}
                onChange={(event) =>
                  setEditForm({ ...editForm, queues: event.target.value })
                }
              />
            </div>
            <div className="grid gap-3 sm:grid-cols-3">
              <div className="space-y-2">
                <Label htmlFor="edit-pool-timeout">Timeout (s)</Label>
                <Input
                  id="edit-pool-timeout"
                  type="number"
                  min={0}
                  max={86400}
                  value={editForm.jobTimeout}
                  onChange={(event) =>
                    setEditForm({ ...editForm, jobTimeout: event.target.value })
                  }
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="edit-pool-attempts">Max attempts</Label>
                <Input
                  id="edit-pool-attempts"
                  type="number"
                  min={0}
                  max={100}
                  value={editForm.maxAttempts}
                  onChange={(event) =>
                    setEditForm({ ...editForm, maxAttempts: event.target.value })
                  }
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="edit-pool-backoff">Backoff</Label>
                <select
                  id="edit-pool-backoff"
                  className="h-8 w-full rounded-lg border border-input bg-transparent px-2.5 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30"
                  value={editForm.retryBackoff}
                  onChange={(event) =>
                    setEditForm({
                      ...editForm,
                      retryBackoff: event.target.value as PoolForm["retryBackoff"],
                    })
                  }
                >
                  <option value="exponential">Exponential</option>
                  <option value="fixed">Fixed</option>
                </select>
              </div>
            </div>
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="edit-pool-retry-base">Retry base (s)</Label>
                <Input
                  id="edit-pool-retry-base"
                  type="number"
                  min={0}
                  max={86400}
                  value={editForm.retryBase}
                  onChange={(event) =>
                    setEditForm({ ...editForm, retryBase: event.target.value })
                  }
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="edit-pool-retry-max">Retry max (s)</Label>
                <Input
                  id="edit-pool-retry-max"
                  type="number"
                  min={0}
                  max={86400}
                  value={editForm.retryMax}
                  onChange={(event) =>
                    setEditForm({ ...editForm, retryMax: event.target.value })
                  }
                />
              </div>
            </div>
            <div className="space-y-2">
              <Label htmlFor="edit-pool-cidrs">Allowed CIDRs</Label>
              <Input
                id="edit-pool-cidrs"
                placeholder="10.0.0.0/8 (blank = any address)"
                value={editForm.allowedCidrs}
                onChange={(event) =>
                  setEditForm({ ...editForm, allowedCidrs: event.target.value })
                }
              />
            </div>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setEditing(null)}>
                Cancel
              </Button>
              <Button type="submit" disabled={updatePool.isPending}>
                {updatePool.isPending && <Loader2 className="size-4 animate-spin" />}
                Save changes
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </Card>
  )
}

function WorkerPoolRow({
  pool,
  stat,
  busy,
  issuing,
  onEdit,
  onToggle,
  onEnroll,
  onDelete,
}: {
  pool: WorkerPool
  stat?: WorkerPoolStat
  busy: boolean
  issuing: boolean
  onEdit: () => void
  onToggle: () => void
  onEnroll: () => void
  onDelete: () => void
}) {
  const active = pool.status === "active"
  const queues = pool.queue_names.length > 0 ? pool.queue_names.join(", ") : "All queues"
  const timeout = pool.job_timeout_seconds > 0 ? `${pool.job_timeout_seconds}s timeout` : "Project timeout"
  const retries =
    pool.max_attempts > 0
      ? `${pool.max_attempts} attempts`
      : "Job retry policy"

  return (
    <div className="flex items-center justify-between gap-3 px-3 py-3">
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <p className="truncate text-sm font-medium">{pool.name}</p>
          <Badge variant={active ? "default" : "secondary"}>
            {active ? "Active" : "Paused"}
          </Badge>
        </div>
        <p className="mt-0.5 truncate text-xs text-muted-foreground">
          Queues: {queues} · Concurrency {pool.concurrency}
        </p>
        <p className="mt-0.5 truncate text-xs text-muted-foreground">
          {timeout} · {retries} · {pool.retry_backoff} backoff · Created{" "}
          {formatDate(pool.created_at)}
        </p>
        {stat && (
          <p className="mt-0.5 truncate text-xs text-muted-foreground">
            {stat.online_workers} online / {stat.offline_workers} offline ·{" "}
            {stat.running_jobs} running · {stat.queue_depth} queued ·{" "}
            {stat.jobs_per_minute.toFixed(1)} jobs/min ·{" "}
            {stat.success_rate.toFixed(1)}% success
          </p>
        )}
        {pool.allowed_cidrs.length > 0 && (
          <p className="mt-0.5 truncate text-xs text-muted-foreground">
            Allowed: {pool.allowed_cidrs.join(", ")}
          </p>
        )}
      </div>
      <div className="flex items-center gap-1">
        <Button
          type="button"
          size="icon-sm"
          variant="ghost"
          onClick={onEdit}
          aria-label={`Edit ${pool.name}`}
          title="Edit"
        >
          <Pencil className="size-3.5" />
        </Button>
        <Button
          type="button"
          size="icon-sm"
          variant="ghost"
          onClick={onEnroll}
          disabled={!active || issuing}
          aria-label={`Create enrollment token for ${pool.name}`}
          title="Create enrollment token"
        >
          {issuing ? (
            <Loader2 className="size-3.5 animate-spin" />
          ) : (
            <KeyRound className="size-3.5" />
          )}
        </Button>
        <Button
          type="button"
          size="icon-sm"
          variant="ghost"
          onClick={onToggle}
          disabled={busy}
          aria-label={active ? `Pause ${pool.name}` : `Resume ${pool.name}`}
          title={active ? "Pause" : "Resume"}
        >
          {busy ? (
            <Loader2 className="size-3.5 animate-spin" />
          ) : active ? (
            <Pause className="size-3.5" />
          ) : (
            <Play className="size-3.5" />
          )}
        </Button>
        <Button
          type="button"
          size="icon-sm"
          variant="ghost"
          onClick={onDelete}
          aria-label={`Delete ${pool.name}`}
          title="Delete"
        >
          <Trash2 className="size-3.5" />
        </Button>
      </div>
    </div>
  )
}
