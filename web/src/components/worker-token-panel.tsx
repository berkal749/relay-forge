import { useState, type FormEvent } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Ban, Copy, Loader2, Plus, RefreshCw, Server } from "lucide-react"
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
import { api, ApiError, type WorkerToken } from "@/lib/api"
import { useAuth } from "@/lib/auth"
import { formatDate } from "@/lib/format"

export function WorkerTokenPanel({ projectId }: { projectId: string }) {
  const { token } = useAuth()
  const queryClient = useQueryClient()
  const [name, setName] = useState("")
  const [expiresInDays, setExpiresInDays] = useState("")
  const [rawToken, setRawToken] = useState("")

  const tokensQuery = useQuery({
    queryKey: ["worker-tokens", projectId],
    queryFn: () => api.listWorkerTokens(token!, projectId),
    enabled: Boolean(token),
  })

  const createToken = useMutation({
    mutationFn: (input: { name: string; expiresInDays?: number }) =>
      api.createWorkerToken(token!, projectId, input.name, input.expiresInDays),
    onSuccess: (result) => {
      setRawToken(result.token)
      setName("")
      setExpiresInDays("")
      queryClient.invalidateQueries({ queryKey: ["worker-tokens", projectId] })
      toast.success("Worker token created")
    },
    onError: (error) => {
      toast.error(error instanceof ApiError ? error.message : "Could not create worker token")
    },
  })

  const rotateToken = useMutation({
    mutationFn: (tokenId: string) => api.rotateWorkerToken(token!, projectId, tokenId),
    onSuccess: (result) => {
      setRawToken(result.token)
      queryClient.invalidateQueries({ queryKey: ["worker-tokens", projectId] })
      toast.success("Worker token rotated; the old token keeps working during the grace period")
    },
    onError: (error) => {
      toast.error(error instanceof ApiError ? error.message : "Could not rotate worker token")
    },
  })

  const revokeToken = useMutation({
    mutationFn: (tokenId: string) => api.revokeWorkerToken(token!, projectId, tokenId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["worker-tokens", projectId] })
      toast.success("Worker token revoked")
    },
    onError: (error) => {
      toast.error(error instanceof ApiError ? error.message : "Could not revoke worker token")
    },
  })

  function handleCreate(event: FormEvent) {
    event.preventDefault()
    if (!name.trim()) return

    let parsedExpiry: number | undefined
    if (expiresInDays.trim() !== "") {
      parsedExpiry = Number(expiresInDays)
      if (!Number.isFinite(parsedExpiry) || parsedExpiry < 1 || parsedExpiry > 3650) {
        toast.error("Expiry must be between 1 and 3650 days")
        return
      }
    }

    createToken.mutate({ name: name.trim(), expiresInDays: parsedExpiry })
  }

  async function copyToken() {
    await navigator.clipboard.writeText(rawToken)
    toast.success("Worker token copied")
  }

  const tokens = tokensQuery.data?.worker_tokens ?? []

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Server className="size-4" />
          Worker tokens
        </CardTitle>
        <CardDescription>
          Credentials that authorize workers to process this project.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        {rawToken && (
          <div className="rounded-lg border border-emerald-500/30 bg-emerald-500/10 p-3">
            <p className="text-xs font-medium text-emerald-700 dark:text-emerald-300">
              Copy this token now. It will not be shown again.
            </p>
            <div className="mt-2 flex items-center gap-2">
              <code className="min-w-0 flex-1 truncate rounded bg-background/60 px-2 py-1.5 font-mono text-[11px]">
                {rawToken}
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
              <Label htmlFor="worker-token-name">New token name</Label>
              <Input
                id="worker-token-name"
                placeholder="Worker pool"
                value={name}
                onChange={(event) => setName(event.target.value)}
                maxLength={100}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="worker-token-expiry">Expires (days)</Label>
              <Input
                id="worker-token-expiry"
                type="number"
                min={1}
                max={3650}
                placeholder="Never"
                value={expiresInDays}
                onChange={(event) => setExpiresInDays(event.target.value)}
              />
            </div>
          </div>
          <Button type="submit" className="w-full" disabled={createToken.isPending}>
            {createToken.isPending ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <Plus className="size-4" />
            )}
            Create worker token
          </Button>
        </form>

        {tokensQuery.isLoading ? (
          <div className="space-y-2">
            <Skeleton className="h-14 rounded-lg" />
            <Skeleton className="h-14 rounded-lg" />
          </div>
        ) : tokens.length === 0 ? (
          <p className="py-2 text-sm text-muted-foreground">
            No worker tokens yet. Create one before starting a worker.
          </p>
        ) : (
          <div className="divide-y rounded-lg border">
            {tokens.map((item) => (
              <WorkerTokenRow
                key={item.id}
                workerToken={item}
                onRevoke={() => revokeToken.mutate(item.id)}
                revoking={revokeToken.isPending && revokeToken.variables === item.id}
                onRotate={() => rotateToken.mutate(item.id)}
                rotating={rotateToken.isPending && rotateToken.variables === item.id}
              />
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function WorkerTokenRow({
  workerToken,
  onRevoke,
  revoking,
  onRotate,
  rotating,
}: {
  workerToken: WorkerToken
  onRevoke: () => void
  revoking: boolean
  onRotate: () => void
  rotating: boolean
}) {
  return (
    <div className="flex items-center justify-between gap-3 px-3 py-3">
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <p className="truncate text-sm font-medium">{workerToken.name}</p>
          {workerToken.revoked_at && <Badge variant="secondary">Revoked</Badge>}
        </div>
        <p className="mt-0.5 truncate font-mono text-xs text-muted-foreground">
          {workerToken.token_prefix}…
        </p>
        <p className="mt-0.5 text-xs text-muted-foreground">
          Created {formatDate(workerToken.created_at)}
          {workerToken.expires_at && ` · Expires ${formatDate(workerToken.expires_at)}`}
          {workerToken.last_used_at && ` · Last used ${formatDate(workerToken.last_used_at)}`}
          {workerToken.rotated_at && ` · Rotated ${formatDate(workerToken.rotated_at)}`}
        </p>
      </div>
      {!workerToken.revoked_at && (
        <div className="flex items-center gap-1">
          <Button
            type="button"
            size="icon-sm"
            variant="ghost"
            onClick={onRotate}
            disabled={rotating}
            aria-label={`Rotate ${workerToken.name}`}
            title="Rotate token"
          >
            {rotating ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : (
              <RefreshCw className="size-3.5" />
            )}
          </Button>
          <Button
            type="button"
            size="icon-sm"
            variant="ghost"
            onClick={onRevoke}
            disabled={revoking}
            aria-label={`Revoke ${workerToken.name}`}
          >
            {revoking ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : (
              <Ban className="size-3.5" />
            )}
          </Button>
        </div>
      )}
    </div>
  )
}
