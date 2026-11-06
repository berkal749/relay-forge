import { useState, type FormEvent } from "react"
import { useNavigate } from "react-router-dom"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Loader2, Send } from "lucide-react"
import { toast } from "sonner"
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
import { Textarea } from "@/components/ui/textarea"
import { api, ApiError } from "@/lib/api"
import { useAuth } from "@/lib/auth"

type SubmitDefaults = {
  queue_name: string
  priority: number
  max_attempts: number
}

export function SubmitJobForm({
  projectId,
  defaults,
}: {
  projectId: string
  defaults: SubmitDefaults
}) {
  const { token } = useAuth()
  const queryClient = useQueryClient()
  const navigate = useNavigate()

  const [queueName, setQueueName] = useState(defaults.queue_name)
  const [jobType, setJobType] = useState("echo")
  const [payload, setPayload] = useState('{\n  "message": "hello"\n}')
  const [priority, setPriority] = useState(defaults.priority)
  const [maxAttempts, setMaxAttempts] = useState(defaults.max_attempts)
  const [scheduledAt, setScheduledAt] = useState("")
  const [error, setError] = useState("")

  const submitJob = useMutation({
    mutationFn: () => {
      let parsedPayload: unknown
      try {
        parsedPayload = JSON.parse(payload)
      } catch {
        throw new Error("Payload must be valid JSON")
      }

      return api.submitJob(token!, projectId, {
        queue_name: queueName.trim(),
        job_type: jobType.trim(),
        payload: parsedPayload,
        priority,
        max_attempts: maxAttempts,
        scheduled_at: scheduledAt
          ? new Date(scheduledAt).toISOString()
          : undefined,
      })
    },
    onSuccess: ({ job }) => {
      queryClient.invalidateQueries({ queryKey: ["jobs", projectId] })
      toast.success("Job submitted")
      navigate(`/projects/${projectId}/jobs/${job.id}`)
    },
    onError: (err) => {
      setError(err instanceof ApiError ? err.message : err.message)
      toast.error(err instanceof ApiError ? err.message : "Could not submit job")
    },
  })

  function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setError("")
    submitJob.mutate()
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Submit a job</CardTitle>
        <CardDescription>
          Queue work for this project. Payload must be valid JSON.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form className="space-y-5" onSubmit={handleSubmit}>
          {error && (
            <div className="rounded-lg border border-destructive/30 bg-destructive/10 px-3 py-2.5 text-sm text-destructive">
              {error}
            </div>
          )}

          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="queue_name">Queue</Label>
              <Input
                id="queue_name"
                value={queueName}
                onChange={(event) => setQueueName(event.target.value)}
                maxLength={100}
                required
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="job_type">Job type</Label>
              <Input
                id="job_type"
                value={jobType}
                onChange={(event) => setJobType(event.target.value)}
                placeholder="send_email"
                maxLength={100}
                required
              />
            </div>
          </div>

          <div className="space-y-2">
            <Label htmlFor="payload">Payload</Label>
            <Textarea
              id="payload"
              className="min-h-32 font-mono text-xs leading-6"
              value={payload}
              onChange={(event) => setPayload(event.target.value)}
              spellCheck={false}
            />
          </div>

          <div className="grid gap-4 sm:grid-cols-3">
            <div className="space-y-2">
              <Label htmlFor="priority">Priority</Label>
              <Input
                id="priority"
                type="number"
                value={priority}
                onChange={(event) => setPriority(Number(event.target.value))}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="max_attempts">Max attempts</Label>
              <Input
                id="max_attempts"
                type="number"
                min={1}
                max={100}
                value={maxAttempts}
                onChange={(event) => setMaxAttempts(Number(event.target.value))}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="scheduled_at">Schedule for</Label>
              <Input
                id="scheduled_at"
                type="datetime-local"
                value={scheduledAt}
                onChange={(event) => setScheduledAt(event.target.value)}
              />
            </div>
          </div>

          <div className="flex justify-end">
            <Button type="submit" disabled={submitJob.isPending}>
              {submitJob.isPending ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <Send className="size-4" />
              )}
              Submit job
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  )
}
