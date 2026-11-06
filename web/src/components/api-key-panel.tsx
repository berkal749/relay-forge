import { useState, type FormEvent } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Copy, KeyRound, Loader2, Plus, Ban } from "lucide-react"
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
import { api, ApiError, type ApiKey } from "@/lib/api"
import { useAuth } from "@/lib/auth"
import { formatDate } from "@/lib/format"

export function ApiKeyPanel({ projectId }: { projectId: string }) {
  const { token } = useAuth()
  const queryClient = useQueryClient()
  const [name, setName] = useState("")
  const [expiresInDays, setExpiresInDays] = useState("")
  const [rawKey, setRawKey] = useState("")

  const keysQuery = useQuery({
    queryKey: ["api-keys", projectId],
    queryFn: () => api.listApiKeys(token!, projectId),
    enabled: Boolean(token),
  })

  const createKey = useMutation({
    mutationFn: (input: { name: string; expiresInDays?: number }) =>
      api.createApiKey(token!, projectId, input.name, input.expiresInDays),
    onSuccess: (result) => {
      setRawKey(result.key)
      setName("")
      setExpiresInDays("")
      queryClient.invalidateQueries({ queryKey: ["api-keys", projectId] })
      toast.success("API key created")
    },
    onError: (error) => {
      toast.error(error instanceof ApiError ? error.message : "Could not create API key")
    },
  })

  const revokeKey = useMutation({
    mutationFn: (keyId: string) => api.revokeApiKey(token!, projectId, keyId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["api-keys", projectId] })
      toast.success("API key revoked")
    },
    onError: (error) => {
      toast.error(error instanceof ApiError ? error.message : "Could not revoke API key")
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

    createKey.mutate({ name: name.trim(), expiresInDays: parsedExpiry })
  }

  async function copyKey() {
    await navigator.clipboard.writeText(rawKey)
    toast.success("API key copied")
  }

  const keys = keysQuery.data?.api_keys ?? []

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <KeyRound className="size-4" />
          API keys
        </CardTitle>
        <CardDescription>
          Machine credentials for submitting jobs to this project.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        {rawKey && (
          <div className="rounded-lg border border-emerald-500/30 bg-emerald-500/10 p-3">
            <p className="text-xs font-medium text-emerald-700 dark:text-emerald-300">
              Copy this key now. It will not be shown again.
            </p>
            <div className="mt-2 flex items-center gap-2">
              <code className="min-w-0 flex-1 truncate rounded bg-background/60 px-2 py-1.5 font-mono text-[11px]">
                {rawKey}
              </code>
              <Button type="button" size="icon-sm" variant="outline" onClick={copyKey}>
                <Copy className="size-3.5" />
              </Button>
            </div>
          </div>
        )}

        <form className="space-y-3" onSubmit={handleCreate}>
          <div className="grid gap-3 sm:grid-cols-[1fr_150px]">
            <div className="space-y-2">
              <Label htmlFor="key-name">New key name</Label>
              <Input
                id="key-name"
                placeholder="Production"
                value={name}
                onChange={(event) => setName(event.target.value)}
                maxLength={100}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="key-expiry">Expires (days)</Label>
              <Input
                id="key-expiry"
                type="number"
                min={1}
                max={3650}
                placeholder="Never"
                value={expiresInDays}
                onChange={(event) => setExpiresInDays(event.target.value)}
              />
            </div>
          </div>
          <Button type="submit" className="w-full" disabled={createKey.isPending}>
            {createKey.isPending ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <Plus className="size-4" />
            )}
            Create API key
          </Button>
        </form>

        {keysQuery.isLoading ? (
          <div className="space-y-2">
            <Skeleton className="h-14 rounded-lg" />
            <Skeleton className="h-14 rounded-lg" />
          </div>
        ) : keys.length === 0 ? (
          <p className="py-2 text-sm text-muted-foreground">
            No API keys yet. Create one to connect a worker or submit jobs.
          </p>
        ) : (
          <div className="divide-y rounded-lg border">
            {keys.map((key) => (
              <ApiKeyRow
                key={key.id}
                apiKey={key}
                onRevoke={() => revokeKey.mutate(key.id)}
                revoking={revokeKey.isPending && revokeKey.variables === key.id}
              />
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function ApiKeyRow({
  apiKey,
  onRevoke,
  revoking,
}: {
  apiKey: ApiKey
  onRevoke: () => void
  revoking: boolean
}) {
  return (
    <div className="flex items-center justify-between gap-3 px-3 py-3">
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <p className="truncate text-sm font-medium">{apiKey.name}</p>
          {apiKey.revoked_at && <Badge variant="secondary">Revoked</Badge>}
        </div>
        <p className="mt-0.5 truncate font-mono text-xs text-muted-foreground">
          {apiKey.key_prefix}…
        </p>
        <p className="mt-0.5 text-xs text-muted-foreground">
          Created {formatDate(apiKey.created_at)}
          {apiKey.expires_at && ` · Expires ${formatDate(apiKey.expires_at)}`}
          {apiKey.last_used_at && ` · Last used ${formatDate(apiKey.last_used_at)}`}
        </p>
      </div>
      {!apiKey.revoked_at && (
        <Button
          type="button"
          size="icon-sm"
          variant="ghost"
          onClick={onRevoke}
          disabled={revoking}
          aria-label={`Revoke ${apiKey.name}`}
        >
          {revoking ? (
            <Loader2 className="size-3.5 animate-spin" />
          ) : (
            <Ban className="size-3.5" />
          )}
        </Button>
      )}
    </div>
  )
}
