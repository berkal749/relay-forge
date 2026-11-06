export type JobTypeGroup = "Actions" | "Notifications" | "Development"

export type JobTypeDefinition = {
  type: string
  label: string
  description: string
  group: JobTypeGroup
  example: Record<string, unknown>
}

export const JOB_TYPES: JobTypeDefinition[] = [
  {
    type: "http.request",
    label: "HTTP request / webhook",
    description: "Call a URL with a method, headers, body, and timeout.",
    group: "Actions",
    example: {
      url: "https://example.com/webhook",
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: { message: "hello" },
      timeout_seconds: 30,
    },
  },
  {
    type: "delay.run",
    label: "Delay / schedule another job",
    description: "Enqueue another job after a delay or at a specific time.",
    group: "Actions",
    example: {
      delay_seconds: 60,
      job: {
        job_type: "http.request",
        payload: { url: "https://example.com/webhook" },
      },
    },
  },
  {
    type: "notify.email",
    label: "Email",
    description: "Send email through the configured SMTP relay.",
    group: "Notifications",
    example: {
      to: ["person@example.com"],
      subject: "Hello from RelayForge",
      text: "Your job finished.",
    },
  },
  {
    type: "notify.slack",
    label: "Slack message",
    description: "Post to a Slack incoming webhook.",
    group: "Notifications",
    example: {
      webhook_url: "https://hooks.slack.com/services/...",
      text: "Deploy finished",
    },
  },
  {
    type: "notify.discord",
    label: "Discord message",
    description: "Post to a Discord webhook.",
    group: "Notifications",
    example: {
      webhook_url: "https://discord.com/api/webhooks/...",
      content: "Deploy finished",
    },
  },
  {
    type: "notify.telegram",
    label: "Telegram message",
    description: "Send a message with the configured bot token.",
    group: "Notifications",
    example: {
      chat_id: "-1001234567890",
      text: "Deploy finished",
    },
  },
  {
    type: "notify.sms",
    label: "SMS (Twilio)",
    description: "Send an SMS through Twilio.",
    group: "Notifications",
    example: {
      to: "+15551234567",
      body: "Code 1234",
    },
  },
  {
    type: "echo",
    label: "Echo",
    description: "Return the payload as the result. Development handler.",
    group: "Development",
    example: { message: "hello" },
  },
  {
    type: "noop",
    label: "No-op",
    description: "Complete immediately without doing anything.",
    group: "Development",
    example: {},
  },
  {
    type: "sleep",
    label: "Sleep",
    description: "Wait for duration_ms. Useful for testing timeouts.",
    group: "Development",
    example: { duration_ms: 1000 },
  },
]

export const JOB_TYPE_GROUPS: JobTypeGroup[] = ["Actions", "Notifications", "Development"]

export const CUSTOM_JOB_TYPE = "__custom__"

export function findJobType(type: string): JobTypeDefinition | undefined {
  return JOB_TYPES.find((definition) => definition.type === type)
}

export function examplePayload(type: string): Record<string, unknown> | null {
  return findJobType(type)?.example ?? null
}

export function examplePayloadJson(type: string): string | null {
  const example = examplePayload(type)
  if (!example) return null
  return JSON.stringify(example, null, 2)
}
