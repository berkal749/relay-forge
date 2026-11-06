import { useQuery } from "@tanstack/react-query"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { api, type NotificationChannelKind } from "@/lib/api"
import { useAuth } from "@/lib/auth"

type Field = {
  key: string
  label: string
  placeholder?: string
  help?: string
  textarea?: boolean
  list?: boolean
  required?: boolean
}

const NOTIFICATION_FIELDS: Record<string, Field[]> = {
  "notify.discord": [
    {
      key: "webhook_url",
      required: true,
      label: "Discord webhook URL",
      placeholder: "https://discord.com/api/webhooks/...",
      help: "Discord → Channel settings → Integrations → Webhooks → Copy Webhook URL.",
    },
    {
      key: "content",
      required: true,
      label: "Message",
      placeholder: "Deploy finished",
      textarea: true,
    },
    {
      key: "username",
      label: "Username (optional)",
      placeholder: "RelayForge",
    },
  ],
  "notify.slack": [
    {
      key: "webhook_url",
      required: true,
      label: "Slack webhook URL",
      placeholder: "https://hooks.slack.com/services/...",
      help: "Slack → Apps → Incoming Webhooks → Copy Webhook URL.",
    },
    {
      key: "text",
      required: true,
      label: "Message",
      placeholder: "Deploy finished",
      textarea: true,
    },
  ],
  "notify.telegram": [
    {
      key: "chat_id",
      required: true,
      label: "Chat ID",
      placeholder: "-1001234567890",
      help: "The bot token is configured on the worker via TELEGRAM_BOT_TOKEN.",
    },
    {
      key: "text",
      required: true,
      label: "Message",
      placeholder: "Deploy finished",
      textarea: true,
    },
  ],
  "notify.sms": [
    {
      key: "to",
      required: true,
      label: "Phone number",
      placeholder: "+15551234567",
      help: "E.164 format. Twilio credentials are configured on the worker.",
    },
    {
      key: "body",
      required: true,
      label: "Message",
      placeholder: "Your code is 1234",
      textarea: true,
    },
  ],
  "notify.email": [
    {
      key: "to",
      required: true,
      label: "Recipients",
      placeholder: "person@example.com, team@example.com",
      help: "Comma-separated. SMTP credentials are configured on the worker.",
      list: true,
    },
    {
      key: "subject",
      required: true,
      label: "Subject",
      placeholder: "Nightly report",
    },
    {
      key: "text",
      required: true,
      label: "Message",
      placeholder: "Everything is green.",
      textarea: true,
    },
  ],
}

const CHANNEL_KIND: Record<string, NotificationChannelKind> = {
  "notify.slack": "slack",
  "notify.discord": "discord",
  "notify.telegram": "telegram",
  "notify.sms": "sms",
  "notify.email": "email",
}

const DESTINATION_KEYS: Record<NotificationChannelKind, string[]> = {
  slack: ["webhook_url"],
  discord: ["webhook_url"],
  telegram: ["chat_id"],
  sms: ["to", "from"],
  email: ["to"],
}

function parsePayload(payload: string): { value: Record<string, unknown>; valid: boolean } {
  try {
    const parsed: unknown = JSON.parse(payload)
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      return { value: parsed as Record<string, unknown>, valid: true }
    }
  } catch {
    // Fall through to an empty object.
  }
  return { value: {}, valid: false }
}

function fieldValue(value: unknown): string {
  if (Array.isArray(value)) return value.join(", ")
  if (value === null || value === undefined) return ""
  if (typeof value === "object") return JSON.stringify(value)
  return String(value)
}

export function JobPayloadFields({
  projectId,
  id,
  jobType,
  payload,
  onChange,
  rows = 6,
}: {
  projectId: string
  id: string
  jobType: string
  payload: string
  onChange: (payload: string) => void
  rows?: number
}) {
  const { token } = useAuth()
  const channelsQuery = useQuery({
    queryKey: ["notification-channels", projectId],
    queryFn: () => api.listNotificationChannels(token!, projectId),
    enabled: Boolean(token && projectId),
  })

  const fields = NOTIFICATION_FIELDS[jobType]
  const parsed = parsePayload(payload)

  if (!fields) {
    return (
      <Textarea
        id={id}
        className="font-mono text-xs leading-6"
        value={payload}
        onChange={(event) => onChange(event.target.value)}
        spellCheck={false}
        rows={rows}
      />
    )
  }

  const kind = CHANNEL_KIND[jobType]
  const channels = (channelsQuery.data?.notification_channels ?? []).filter(
    (channel) => channel.kind === kind,
  )
  const channelID =
    typeof parsed.value.channel_id === "string" ? parsed.value.channel_id : ""
  const destinationKeys = DESTINATION_KEYS[kind] ?? []
  const selectedChannel = channels.some((channel) => channel.id === channelID)
    ? channelID
    : ""

  function chooseDestination(value: string) {
    const next = { ...parsed.value }
    for (const key of destinationKeys) {
      delete next[key]
    }
    if (value) {
      next.channel_id = value
    } else {
      delete next.channel_id
    }
    onChange(JSON.stringify(next, null, 2))
  }

  function updateField(field: Field, raw: string) {
    const next = { ...parsed.value }
    if (field.list) {
      const values = raw
        .split(",")
        .map((value) => value.trim())
        .filter(Boolean)
      if (values.length === 0) {
        delete next[field.key]
      } else {
        next[field.key] = values
      }
    } else if (raw.trim() === "") {
      delete next[field.key]
    } else {
      next[field.key] = raw
    }
    onChange(JSON.stringify(next, null, 2))
  }

  return (
    <div className="space-y-4">
      {!parsed.valid && (
        <p className="rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-300">
          The current payload is not a JSON object. Editing a field below will
          replace it.
        </p>
      )}

      {channels.length > 0 && (
        <div className="space-y-2 rounded-lg border bg-muted/30 p-3">
          <Label htmlFor={`${id}-destination`}>Destination</Label>
          <select
            id={`${id}-destination`}
            className="h-9 w-full rounded-md border bg-background px-3 text-sm"
            value={selectedChannel}
            onChange={(event) => chooseDestination(event.target.value)}
          >
            <option value="">Enter manually</option>
            {channels.map((channel) => (
              <option key={channel.id} value={channel.id}>
                {channel.name}
              </option>
            ))}
          </select>
          <p className="text-xs text-muted-foreground">
            {selectedChannel
              ? "The destination is stored encrypted in this project and is not included in the job payload."
              : "Pick a saved channel, or enter the destination below."}
          </p>
        </div>
      )}

      <div className="grid gap-4 sm:grid-cols-2">
        {fields
          .filter((field) => !(channelID && destinationKeys.includes(field.key)))
          .map((field) => {
          const fieldID = `${id}-${field.key}`
          const value = fieldValue(parsed.value[field.key])
          const className = field.textarea ? "space-y-2 sm:col-span-2" : "space-y-2"
          return (
            <div key={field.key} className={className}>
              <Label htmlFor={fieldID}>{field.label}</Label>
              {field.textarea ? (
                <Textarea
                  id={fieldID}
                  rows={3}
                  className="text-sm"
                  placeholder={field.placeholder}
                  value={value}
                  onChange={(event) => updateField(field, event.target.value)}
                  required={field.required}
                />
              ) : (
                <Input
                  id={fieldID}
                  placeholder={field.placeholder}
                  value={value}
                  onChange={(event) => updateField(field, event.target.value)}
                  required={field.required}
                />
              )}
              {field.help && (
                <p className="text-xs text-muted-foreground">{field.help}</p>
              )}
            </div>
          )
        })}
      </div>

      <details className="group">
        <summary className="cursor-pointer text-xs text-muted-foreground hover:text-foreground">
          Advanced JSON
        </summary>
        <Textarea
          id={id}
          className="mt-2 font-mono text-xs leading-6"
          value={payload}
          onChange={(event) => onChange(event.target.value)}
          spellCheck={false}
          rows={rows}
        />
      </details>
    </div>
  )
}
