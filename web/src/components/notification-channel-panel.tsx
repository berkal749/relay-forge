import { useState, type FormEvent } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Loader2, Lock, Plus, Trash2 } from "lucide-react"
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
import {
  api,
  ApiError,
  type NotificationChannelKind,
} from "@/lib/api"
import { useAuth } from "@/lib/auth"
import { formatDate } from "@/lib/format"

type ChannelForm = {
  name: string
  kind: NotificationChannelKind
  webhookURL: string
  chatID: string
  to: string
  from: string
}

const emptyForm: ChannelForm = {
  name: "",
  kind: "discord",
  webhookURL: "",
  chatID: "",
  to: "",
  from: "",
}

const KIND_LABELS: Record<NotificationChannelKind, string> = {
  slack: "Slack",
  discord: "Discord",
  telegram: "Telegram",
  sms: "SMS",
  email: "Email",
}

function buildConfig(form: ChannelForm): Record<string, string> {
  switch (form.kind) {
    case "slack":
    case "discord":
      return { webhook_url: form.webhookURL.trim() }
    case "telegram":
      return { chat_id: form.chatID.trim() }
    case "sms":
      return { to: form.to.trim(), ...(form.from.trim() ? { from: form.from.trim() } : {}) }
    case "email":
      return { to: form.to.trim() }
  }
}

function destinationLabel(kind: NotificationChannelKind) {
  switch (kind) {
    case "slack":
    case "discord":
      return "Webhook URL"
    case "telegram":
      return "Chat ID"
    case "sms":
      return "Phone number"
    case "email":
      return "Recipients"
  }
}

export function NotificationChannelPanel({ projectId }: { projectId: string }) {
  const { token } = useAuth()
  const queryClient = useQueryClient()
  const [form, setForm] = useState<ChannelForm>(emptyForm)

  const channelsQuery = useQuery({
    queryKey: ["notification-channels", projectId],
    queryFn: () => api.listNotificationChannels(token!, projectId),
    enabled: Boolean(token),
  })

  const refresh = () =>
    queryClient.invalidateQueries({
      queryKey: ["notification-channels", projectId],
    })

  const createChannel = useMutation({
    mutationFn: () =>
      api.createNotificationChannel(token!, projectId, {
        name: form.name.trim(),
        kind: form.kind,
        config: buildConfig(form),
      }),
    onSuccess: () => {
      setForm({ ...emptyForm, kind: form.kind })
      refresh()
      toast.success("Notification channel created")
    },
    onError: (error) => {
      toast.error(
        error instanceof ApiError ? error.message : "Could not create channel",
      )
    },
  })

  const deleteChannel = useMutation({
    mutationFn: (channelId: string) =>
      api.deleteNotificationChannel(token!, projectId, channelId),
    onSuccess: () => {
      refresh()
      toast.success("Notification channel deleted")
    },
    onError: (error) => {
      toast.error(
        error instanceof ApiError ? error.message : "Could not delete channel",
      )
    },
  })

  function handleSubmit(event: FormEvent) {
    event.preventDefault()
    if (!form.name.trim()) {
      toast.error("Channel name is required")
      return
    }
    const config = buildConfig(form)
    if (Object.values(config).some((value) => !value)) {
      toast.error(`${destinationLabel(form.kind)} is required`)
      return
    }
    createChannel.mutate()
  }

  const channels = channelsQuery.data?.notification_channels ?? []

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Lock className="size-4" />
          Notification channels
        </CardTitle>
        <CardDescription>
          Named destinations for Slack, Discord, Telegram, SMS, and email. The
          destination is encrypted at rest and never included in job payloads.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        <form className="space-y-3" onSubmit={handleSubmit}>
          <div className="grid gap-3 sm:grid-cols-[1fr_180px]">
            <div className="space-y-2">
              <Label htmlFor="channel-name">Name</Label>
              <Input
                id="channel-name"
                placeholder="Deploy alerts"
                value={form.name}
                onChange={(event) => setForm({ ...form, name: event.target.value })}
                maxLength={100}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="channel-kind">Kind</Label>
              <select
                id="channel-kind"
                className="h-9 w-full rounded-md border bg-background px-3 text-sm"
                value={form.kind}
                onChange={(event) =>
                  setForm({
                    ...emptyForm,
                    name: form.name,
                    kind: event.target.value as NotificationChannelKind,
                  })
                }
              >
                {(Object.keys(KIND_LABELS) as NotificationChannelKind[]).map(
                  (kind) => (
                    <option key={kind} value={kind}>
                      {KIND_LABELS[kind]}
                    </option>
                  ),
                )}
              </select>
            </div>
          </div>

          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-2 sm:col-span-2">
              <Label htmlFor="channel-destination">
                {destinationLabel(form.kind)}
              </Label>
              <Input
                id="channel-destination"
                placeholder={
                  form.kind === "discord"
                    ? "https://discord.com/api/webhooks/..."
                    : form.kind === "slack"
                      ? "https://hooks.slack.com/services/..."
                      : form.kind === "telegram"
                        ? "-1001234567890"
                        : form.kind === "sms"
                          ? "+15551234567"
                          : "alerts@example.com, oncall@example.com"
                }
                value={
                  form.kind === "telegram"
                    ? form.chatID
                    : form.kind === "slack" || form.kind === "discord"
                      ? form.webhookURL
                      : form.to
                }
                onChange={(event) => {
                  const value = event.target.value
                  setForm((current) => {
                    switch (current.kind) {
                      case "telegram":
                        return { ...current, chatID: value }
                      case "slack":
                      case "discord":
                        return { ...current, webhookURL: value }
                      default:
                        return { ...current, to: value }
                    }
                  })
                }}
              />
              {form.kind === "email" && (
                <p className="text-xs text-muted-foreground">
                  Comma-separated recipients.
                </p>
              )}
              {(form.kind === "discord" || form.kind === "slack") && (
                <p className="text-xs text-muted-foreground">
                  Create the webhook in the channel settings and paste its URL.
                  It is stored encrypted.
                </p>
              )}
            </div>

            {form.kind === "sms" && (
              <div className="space-y-2">
                <Label htmlFor="channel-from">From (optional)</Label>
                <Input
                  id="channel-from"
                  placeholder="+15550000000"
                  value={form.from}
                  onChange={(event) =>
                    setForm({ ...form, from: event.target.value })
                  }
                />
              </div>
            )}
          </div>

          <Button type="submit" disabled={createChannel.isPending}>
            {createChannel.isPending ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <Plus className="size-4" />
            )}
            Add channel
          </Button>
        </form>

        {channelsQuery.isLoading ? (
          <div className="space-y-2">
            <Skeleton className="h-10 rounded-lg" />
            <Skeleton className="h-10 rounded-lg" />
          </div>
        ) : channelsQuery.isError ? (
          <p className="text-sm text-destructive">
            {channelsQuery.error instanceof ApiError
              ? channelsQuery.error.message
              : "Could not load channels"}
          </p>
        ) : channels.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            No channels yet. Add one to let users pick a destination instead of
            pasting a webhook URL.
          </p>
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Kind</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead className="w-10" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {channels.map((channel) => (
                  <TableRow key={channel.id}>
                    <TableCell className="font-medium">{channel.name}</TableCell>
                    <TableCell>
                      <Badge variant="secondary">
                        {KIND_LABELS[channel.kind]}
                      </Badge>
                    </TableCell>
                    <TableCell className="whitespace-nowrap text-muted-foreground">
                      {formatDate(channel.created_at)}
                    </TableCell>
                    <TableCell>
                      <Button
                        variant="ghost"
                        size="sm"
                        aria-label="Delete channel"
                        disabled={deleteChannel.isPending}
                        onClick={() => deleteChannel.mutate(channel.id)}
                      >
                        <Trash2 className="size-4" />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
