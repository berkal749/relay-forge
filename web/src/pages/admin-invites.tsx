import { useState, type FormEvent } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Copy, Loader2, Ticket, Trash2 } from "lucide-react"
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { api, ApiError, type CreatedInvite } from "@/lib/api"
import { useAuth } from "@/lib/auth"
import { formatDate } from "@/lib/format"

export function AdminInvitesPage() {
  const { token, user } = useAuth()
  const queryClient = useQueryClient()
  const [email, setEmail] = useState("")
  const [role, setRole] = useState<"user" | "admin">("user")
  const [created, setCreated] = useState<CreatedInvite | null>(null)

  const invitesQuery = useQuery({
    queryKey: ["invites"],
    queryFn: () => api.listInvites(token!),
    enabled: Boolean(token),
  })
  const [now] = useState(Date.now)

  const createInvite = useMutation({
    mutationFn: () =>
      api.createInvite(token!, { email: email.trim() || undefined, role }),
    onSuccess: (result) => {
      setCreated(result)
      setEmail("")
      queryClient.invalidateQueries({ queryKey: ["invites"] })
      toast.success("Invite created")
    },
    onError: (error) => {
      toast.error(error instanceof ApiError ? error.message : "Could not create invite")
    },
  })

  const revokeInvite = useMutation({
    mutationFn: (id: string) => api.revokeInvite(token!, id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["invites"] })
      toast.success("Invite revoked")
    },
    onError: (error) => {
      toast.error(error instanceof ApiError ? error.message : "Could not revoke invite")
    },
  })

  function handleSubmit(event: FormEvent) {
    event.preventDefault()
    createInvite.mutate()
  }

  async function copyToken(value: string) {
    await navigator.clipboard.writeText(value)
    toast.success("Invite token copied")
  }

  if (user?.role !== "admin") {
    return (
      <Card>
        <CardContent className="py-12 text-center">
          <p className="font-medium">Administrator access required</p>
          <p className="mt-1 text-sm text-muted-foreground">
            Ask an administrator if you need an invite.
          </p>
        </CardContent>
      </Card>
    )
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Invites</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          Create single-use registration links for the beta.
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Ticket className="size-4" />
            New invite
          </CardTitle>
          <CardDescription>
            Leave the email blank to allow any address. Invites expire after seven days.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form className="flex flex-col gap-4 sm:flex-row sm:items-end" onSubmit={handleSubmit}>
            <div className="flex-1 space-y-2">
              <Label htmlFor="invite-email">Email (optional)</Label>
              <Input
                id="invite-email"
                type="email"
                placeholder="person@example.com"
                value={email}
                onChange={(event) => setEmail(event.target.value)}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="invite-role">Role</Label>
              <select
                id="invite-role"
                className="h-9 rounded-md border bg-background px-3 text-sm"
                value={role}
                onChange={(event) => setRole(event.target.value as "user" | "admin")}
              >
                <option value="user">User</option>
                <option value="admin">Admin</option>
              </select>
            </div>
            <Button type="submit" disabled={createInvite.isPending}>
              {createInvite.isPending ? <Loader2 className="size-4 animate-spin" /> : null}
              Create invite
            </Button>
          </form>

          {created && (
            <div className="mt-4 rounded-lg border bg-muted/40 p-3">
              <p className="text-xs font-medium text-muted-foreground">
                Invite token (shown once)
              </p>
              <div className="mt-2 flex items-center gap-2">
                <code className="flex-1 overflow-x-auto font-mono text-xs">
                  {created.token}
                </code>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => copyToken(created.token)}
                >
                  <Copy className="size-3.5" />
                  Copy
                </Button>
              </div>
              <p className="mt-2 text-xs text-muted-foreground">
                Share this link: {window.location.origin}/register?invite={created.token}
              </p>
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Issued invites</CardTitle>
          <CardDescription>Consumed and expired invites remain listed for audit.</CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          {invitesQuery.isLoading ? (
            <div className="space-y-2 p-4">
              <Skeleton className="h-10 rounded-lg" />
              <Skeleton className="h-10 rounded-lg" />
            </div>
          ) : invitesQuery.isError ? (
            <p className="p-4 text-sm text-destructive">
              {invitesQuery.error instanceof ApiError
                ? invitesQuery.error.message
                : "Could not load invites"}
            </p>
          ) : (invitesQuery.data?.invites.length ?? 0) === 0 ? (
            <p className="p-4 text-sm text-muted-foreground">No invites yet.</p>
          ) : (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Email</TableHead>
                    <TableHead>Role</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead>Expires</TableHead>
                    <TableHead className="w-10" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {invitesQuery.data?.invites.map((invite) => {
                    const expired = Date.parse(invite.expires_at) <= now
                    return (
                      <TableRow key={invite.id}>
                        <TableCell className="font-medium">
                          {invite.email ?? "Any email"}
                        </TableCell>
                        <TableCell className="capitalize text-muted-foreground">
                          {invite.role}
                        </TableCell>
                        <TableCell>
                          {invite.consumed_at ? (
                            <Badge variant="secondary">Consumed</Badge>
                          ) : expired ? (
                            <Badge variant="secondary">Expired</Badge>
                          ) : (
                            <Badge>Pending</Badge>
                          )}
                        </TableCell>
                        <TableCell className="whitespace-nowrap text-muted-foreground">
                          {formatDate(invite.expires_at)}
                        </TableCell>
                        <TableCell>
                          <Button
                            variant="ghost"
                            size="sm"
                            disabled={revokeInvite.isPending}
                            onClick={() => revokeInvite.mutate(invite.id)}
                            aria-label="Revoke invite"
                          >
                            <Trash2 className="size-4" />
                          </Button>
                        </TableCell>
                      </TableRow>
                    )
                  })}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
