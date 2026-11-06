import { useState, type FormEvent } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Loader2 } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
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
import { api, ApiError, type Project, type ProjectUpdate } from "@/lib/api"
import { useAuth } from "@/lib/auth"

type Props = {
  project: Project
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function ProjectSettingsDialog({ project, open, onOpenChange }: Props) {
  const { token } = useAuth()
  const queryClient = useQueryClient()

  const [defaultQueueName, setDefaultQueueName] = useState(project.default_queue_name)
  const [defaultPriority, setDefaultPriority] = useState(project.default_priority)
  const [defaultMaxAttempts, setDefaultMaxAttempts] = useState(project.default_max_attempts)
  const [jobRetentionDays, setJobRetentionDays] = useState(project.job_retention_days)
  const [maxConcurrency, setMaxConcurrency] = useState(project.max_concurrency)
  const [rateLimitPerMinute, setRateLimitPerMinute] = useState(project.rate_limit_per_minute)
  const [jobTimeoutSeconds, setJobTimeoutSeconds] = useState(project.default_job_timeout_seconds)

  const saveSettings = useMutation({
    mutationFn: (input: ProjectUpdate) => api.updateProject(token!, project.id, input),
    onSuccess: (result) => {
      queryClient.setQueryData(["project", project.id], result)
      queryClient.invalidateQueries({ queryKey: ["projects"] })
      onOpenChange(false)
      toast.success("Project settings saved")
    },
    onError: (error) => {
      toast.error(error instanceof ApiError ? error.message : "Could not save settings")
    },
  })

  function resetFields() {
    setDefaultQueueName(project.default_queue_name)
    setDefaultPriority(project.default_priority)
    setDefaultMaxAttempts(project.default_max_attempts)
    setJobRetentionDays(project.job_retention_days)
    setMaxConcurrency(project.max_concurrency)
    setRateLimitPerMinute(project.rate_limit_per_minute)
    setJobTimeoutSeconds(project.default_job_timeout_seconds)
  }

  function handleOpenChange(nextOpen: boolean) {
    if (nextOpen) {
      resetFields()
    }
    onOpenChange(nextOpen)
  }

  function handleSubmit(event: FormEvent) {
    event.preventDefault()
    saveSettings.mutate({
      default_queue_name: defaultQueueName.trim(),
      default_priority: defaultPriority,
      default_max_attempts: defaultMaxAttempts,
      job_retention_days: jobRetentionDays,
      max_concurrency: maxConcurrency,
      rate_limit_per_minute: rateLimitPerMinute,
      default_job_timeout_seconds: jobTimeoutSeconds,
    })
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-xl">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>Project settings</DialogTitle>
            <DialogDescription>
              Defaults apply to new jobs. Use 0 for limits to fall back to worker
              defaults or disable the behavior.
            </DialogDescription>
          </DialogHeader>

          <div className="grid gap-5 py-5 sm:grid-cols-2">
            <div className="space-y-2 sm:col-span-2">
              <Label htmlFor="settings-queue">Default queue</Label>
              <Input
                id="settings-queue"
                value={defaultQueueName}
                onChange={(event) => setDefaultQueueName(event.target.value)}
                maxLength={100}
                required
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="settings-priority">Default priority</Label>
              <Input
                id="settings-priority"
                type="number"
                value={defaultPriority}
                onChange={(event) => setDefaultPriority(Number(event.target.value))}
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="settings-attempts">Default max attempts</Label>
              <Input
                id="settings-attempts"
                type="number"
                min={1}
                max={100}
                value={defaultMaxAttempts}
                onChange={(event) => setDefaultMaxAttempts(Number(event.target.value))}
                required
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="settings-retention">Job retention (days)</Label>
              <Input
                id="settings-retention"
                type="number"
                min={0}
                max={3650}
                value={jobRetentionDays}
                onChange={(event) => setJobRetentionDays(Number(event.target.value))}
              />
              <p className="text-xs text-muted-foreground">0 keeps history forever.</p>
            </div>

            <div className="space-y-2">
              <Label htmlFor="settings-concurrency">Max concurrency</Label>
              <Input
                id="settings-concurrency"
                type="number"
                min={0}
                max={1000}
                value={maxConcurrency}
                onChange={(event) => setMaxConcurrency(Number(event.target.value))}
              />
              <p className="text-xs text-muted-foreground">0 uses the worker&apos;s configured value.</p>
            </div>

            <div className="space-y-2">
              <Label htmlFor="settings-rate-limit">Rate limit (jobs/min)</Label>
              <Input
                id="settings-rate-limit"
                type="number"
                min={0}
                max={1000000}
                value={rateLimitPerMinute}
                onChange={(event) => setRateLimitPerMinute(Number(event.target.value))}
              />
              <p className="text-xs text-muted-foreground">0 means unlimited.</p>
            </div>

            <div className="space-y-2">
              <Label htmlFor="settings-timeout">Default job timeout (seconds)</Label>
              <Input
                id="settings-timeout"
                type="number"
                min={0}
                max={86400}
                value={jobTimeoutSeconds}
                onChange={(event) => setJobTimeoutSeconds(Number(event.target.value))}
              />
              <p className="text-xs text-muted-foreground">0 uses the worker&apos;s JOB_TIMEOUT.</p>
            </div>
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={saveSettings.isPending}>
              {saveSettings.isPending && <Loader2 className="size-4 animate-spin" />}
              Save settings
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
