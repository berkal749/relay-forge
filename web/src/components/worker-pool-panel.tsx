import { useState, type FormEvent } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  Copy,
  KeyRound,
  Layers,
  Loader2,
  Pause,
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
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import { api, ApiError, type WorkerPool } from "@/lib/api"
import { useAuth } from "@/lib/auth"
import { formatDate } from "@/lib/format"

export function WorkerPoolPanel({ projectId }: { projectId: string }) {
  const { token } = useAuth()
  const queryClient = useQueryClient()
  const [name, setName] = useState("")
  const [concurrency, setConcurrency] = useState("4")
  const [enrollmentToken, setEnrollmentToken] = useState("")

  const poolsQuery = useQuery({
    queryKey: ["worker-pools", projectId],
    queryFn: () => api.listWorkerPools(token!, projectId),
    enabled: Boolean(token),
  })

  const refresh = () =>
    queryClient.invalidateQueries({ queryKey: ["worker-pools", projectId] })

  const createPool = useMutation({
    mutationFn: (input: { name: string; concurrency: number }) =>
      api.createWorkerPool(token!, projectId, input.name, input.concurrency),
    onSuccess: () => {
      setName("")
      refresh()
      toast.success("Worker pool created")
    },
    onError: (error) => {
      toast.error(error instanceof ApiError ? error.message : "Could not create worker pool")
    },
  })

  const updatePool = useMutation({
    mutationFn: (input: { poolId: string; status: "active" | "paused" }) =>
      api.updateWorkerPool(token!, projectId, input.poolId, { status: input.status }),
    onSuccess: () => {
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
    if (!name.trim()) return

    const parsed = Number(concurrency)
    if (!Number.isInteger(parsed) || parsed < 1 || parsed > 1000) {
      toast.error("Concurrency must be between 1 and 1000")
      return
    }
    createPool.mutate({ name: name.trim(), concurrency: parsed })
  }

  async function copyToken() {
    await navigator.clipboard.writeText(enrollmentToken)
    toast.success("Enrollment token copied")
  }

  const pools = poolsQuery.data?.worker_pools ?? []

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
                value={name}
                onChange={(event) => setName(event.target.value)}
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
                value={concurrency}
                onChange={(event) => setConcurrency(event.target.value)}
              />
            </div>
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
                busy={
                  updatePool.isPending && updatePool.variables?.poolId === pool.id
                }
                issuing={
                  issueEnrollment.isPending && issueEnrollment.variables === pool.id
                }
                onToggle={() =>
                  updatePool.mutate({
                    poolId: pool.id,
                    status: pool.status === "active" ? "paused" : "active",
                  })
                }
                onEnroll={() => issueEnrollment.mutate(pool.id)}
                onDelete={() => deletePool.mutate(pool.id)}
              />
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function WorkerPoolRow({
  pool,
  busy,
  issuing,
  onToggle,
  onEnroll,
  onDelete,
}: {
  pool: WorkerPool
  busy: boolean
  issuing: boolean
  onToggle: () => void
  onEnroll: () => void
  onDelete: () => void
}) {
  const active = pool.status === "active"

  return (
    <div className="flex items-center justify-between gap-3 px-3 py-3">
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <p className="truncate text-sm font-medium">{pool.name}</p>
          <Badge variant={active ? "default" : "secondary"}>
            {active ? "Active" : "Paused"}
          </Badge>
        </div>
        <p className="mt-0.5 text-xs text-muted-foreground">
          Concurrency {pool.concurrency} · Created {formatDate(pool.created_at)}
        </p>
      </div>
      <div className="flex items-center gap-1">
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
