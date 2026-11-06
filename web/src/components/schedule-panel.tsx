import { useState, type FormEvent } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { CalendarClock, Loader2, Pause, Pencil, Play, Plus, Trash2 } from "lucide-react"
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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { api, ApiError, type Schedule, type ScheduleInput } from "@/lib/api"
import { useAuth } from "@/lib/auth"
import { formatDate } from "@/lib/format"

type ScheduleForm = {
  name: string
  cron: string
  timezone: string
  jobType: string
  queueName: string
  payload: string
  priority: string
  maxAttempts: string
}

const emptyForm: ScheduleForm = {
  name: "",
  cron: "*/5 * * * *",
  timezone: "UTC",
  jobType: "http.request",
  queueName: "",
  payload: "{}",
  priority: "",
  maxAttempts: "",
}

function formFromSchedule(schedule: Schedule): ScheduleForm {
  return {
    name: schedule.name,
    cron: schedule.cron,
    timezone: schedule.timezone,
    jobType: schedule.job_type,
    queueName: schedule.queue_name,
    payload: JSON.stringify(schedule.payload ?? {}, null, 2),
    priority: schedule.priority ? String(schedule.priority) : "",
    maxAttempts: schedule.max_attempts ? String(schedule.max_attempts) : "",
  }
}

function toInput(form: ScheduleForm): ScheduleInput {
  let payload: unknown = {}
  const raw = form.payload.trim()
  if (raw) {
    payload = JSON.parse(raw)
  }
  return {
    name: form.name.trim(),
    cron: form.cron.trim(),
    timezone: form.timezone.trim() || "UTC",
    job_type: form.jobType.trim(),
    queue_name: form.queueName.trim(),
    payload,
    priority: form.priority.trim() ? Number(form.priority) : 0,
    max_attempts: form.maxAttempts.trim() ? Number(form.maxAttempts) : 0,
  }
}

function validateForm(form: ScheduleForm): string | null {
  if (!form.name.trim()) return "Name is required"
  if (!form.cron.trim()) return "Cron expression is required"
  if (!form.jobType.trim()) return "Job type is required"
  try {
    const parsed = JSON.parse(form.payload.trim() || "{}")
    if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
      return "Payload must be a JSON object"
    }
  } catch {
    return "Payload is not valid JSON"
  }
  return null
}

export function SchedulePanel({ projectId }: { projectId: string }) {
  const { token } = useAuth()
  const queryClient = useQueryClient()
  const [form, setForm] = useState<ScheduleForm>(emptyForm)
  const [editing, setEditing] = useState<Schedule | null>(null)
  const [editForm, setEditForm] = useState<ScheduleForm>(emptyForm)

  const schedulesQuery = useQuery({
    queryKey: ["schedules", projectId],
    queryFn: () => api.listSchedules(token!, projectId),
    enabled: Boolean(token),
    refetchInterval: 15000,
  })

  const refresh = () =>
    queryClient.invalidateQueries({ queryKey: ["schedules", projectId] })

  const createSchedule = useMutation({
    mutationFn: (input: ScheduleInput) =>
      api.createSchedule(token!, projectId, input),
    onSuccess: () => {
      setForm(emptyForm)
      refresh()
      toast.success("Schedule created")
    },
    onError: (error) => {
      toast.error(error instanceof ApiError ? error.message : "Could not create schedule")
    },
  })

  const updateSchedule = useMutation({
    mutationFn: (input: { scheduleId: string; values: ScheduleInput }) =>
      api.updateSchedule(token!, projectId, input.scheduleId, input.values),
    onSuccess: () => {
      setEditing(null)
      refresh()
      toast.success("Schedule updated")
    },
    onError: (error) => {
      toast.error(error instanceof ApiError ? error.message : "Could not update schedule")
    },
  })

  const toggleSchedule = useMutation({
    mutationFn: (input: { scheduleId: string; enabled: boolean }) =>
      api.updateSchedule(token!, projectId, input.scheduleId, {
        enabled: input.enabled,
      }),
    onSuccess: () => {
      refresh()
      toast.success("Schedule updated")
    },
    onError: (error) => {
      toast.error(error instanceof ApiError ? error.message : "Could not update schedule")
    },
  })

  const deleteSchedule = useMutation({
    mutationFn: (scheduleId: string) =>
      api.deleteSchedule(token!, projectId, scheduleId),
    onSuccess: () => {
      refresh()
      toast.success("Schedule deleted")
    },
    onError: (error) => {
      toast.error(error instanceof ApiError ? error.message : "Could not delete schedule")
    },
  })

  function handleCreate(event: FormEvent) {
    event.preventDefault()
    const problem = validateForm(form)
    if (problem) {
      toast.error(problem)
      return
    }
    try {
      createSchedule.mutate(toInput(form))
    } catch {
      toast.error("Payload is not valid JSON")
    }
  }

  function handleEdit(event: FormEvent) {
    event.preventDefault()
    if (!editing) return
    const problem = validateForm(editForm)
    if (problem) {
      toast.error(problem)
      return
    }
    try {
      updateSchedule.mutate({
        scheduleId: editing.id,
        values: toInput(editForm),
      })
    } catch {
      toast.error("Payload is not valid JSON")
    }
  }

  const schedules = schedulesQuery.data?.schedules ?? []

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <CalendarClock className="size-4" />
          Schedules
        </CardTitle>
        <CardDescription>
          Cron expressions evaluated by the API. Each run submits a new job using
          the template below. Times are computed in the schedule's timezone.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        <form className="space-y-3" onSubmit={handleCreate}>
          <div className="grid gap-3 sm:grid-cols-[1fr_200px_150px]">
            <div className="space-y-2">
              <Label htmlFor="schedule-name">Name</Label>
              <Input
                id="schedule-name"
                placeholder="Nightly report"
                value={form.name}
                onChange={(event) => setForm({ ...form, name: event.target.value })}
                maxLength={100}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="schedule-cron">Cron</Label>
              <Input
                id="schedule-cron"
                placeholder="0 2 * * *"
                className="font-mono text-xs"
                value={form.cron}
                onChange={(event) => setForm({ ...form, cron: event.target.value })}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="schedule-timezone">Timezone</Label>
              <Input
                id="schedule-timezone"
                placeholder="UTC"
                value={form.timezone}
                onChange={(event) => setForm({ ...form, timezone: event.target.value })}
              />
            </div>
          </div>

          <div className="grid gap-3 sm:grid-cols-[1fr_1fr_130px_130px]">
            <div className="space-y-2">
              <Label htmlFor="schedule-job-type">Job type</Label>
              <Input
                id="schedule-job-type"
                placeholder="http.request"
                className="font-mono text-xs"
                value={form.jobType}
                onChange={(event) => setForm({ ...form, jobType: event.target.value })}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="schedule-queue">Queue</Label>
              <Input
                id="schedule-queue"
                placeholder="default"
                value={form.queueName}
                onChange={(event) => setForm({ ...form, queueName: event.target.value })}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="schedule-priority">Priority</Label>
              <Input
                id="schedule-priority"
                type="number"
                min={0}
                max={1000}
                placeholder="Default"
                value={form.priority}
                onChange={(event) => setForm({ ...form, priority: event.target.value })}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="schedule-attempts">Max attempts</Label>
              <Input
                id="schedule-attempts"
                type="number"
                min={0}
                max={100}
                placeholder="Default"
                value={form.maxAttempts}
                onChange={(event) => setForm({ ...form, maxAttempts: event.target.value })}
              />
            </div>
          </div>

          <div className="space-y-2">
            <Label htmlFor="schedule-payload">Job payload (JSON)</Label>
            <textarea
              id="schedule-payload"
              rows={5}
              className="w-full rounded-md border bg-background px-3 py-2 font-mono text-xs"
              value={form.payload}
              onChange={(event) => setForm({ ...form, payload: event.target.value })}
            />
          </div>

          <Button type="submit" disabled={createSchedule.isPending}>
            {createSchedule.isPending ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <Plus className="size-4" />
            )}
            Create schedule
          </Button>
        </form>

        {schedulesQuery.isLoading ? (
          <div className="space-y-2">
            <Skeleton className="h-10 rounded-lg" />
            <Skeleton className="h-10 rounded-lg" />
          </div>
        ) : schedulesQuery.isError ? (
          <p className="text-sm text-destructive">
            {schedulesQuery.error instanceof ApiError
              ? schedulesQuery.error.message
              : "Could not load schedules"}
          </p>
        ) : schedules.length === 0 ? (
          <p className="text-sm text-muted-foreground">No schedules yet.</p>
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Cron</TableHead>
                  <TableHead>Job type</TableHead>
                  <TableHead>Next run</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="w-24" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {schedules.map((schedule) => (
                  <TableRow key={schedule.id}>
                    <TableCell className="font-medium">{schedule.name}</TableCell>
                    <TableCell className="font-mono text-xs">
                      {schedule.cron}
                      <span className="ml-2 text-muted-foreground">
                        {schedule.timezone}
                      </span>
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      {schedule.job_type}
                    </TableCell>
                    <TableCell className="whitespace-nowrap text-muted-foreground">
                      {schedule.enabled ? formatDate(schedule.next_run_at) : "Paused"}
                    </TableCell>
                    <TableCell>
                      <Badge variant={schedule.enabled ? "default" : "secondary"}>
                        {schedule.enabled ? "Active" : "Paused"}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center justify-end gap-1">
                        <Button
                          variant="ghost"
                          size="sm"
                          aria-label={schedule.enabled ? "Pause schedule" : "Resume schedule"}
                          disabled={toggleSchedule.isPending}
                          onClick={() =>
                            toggleSchedule.mutate({
                              scheduleId: schedule.id,
                              enabled: !schedule.enabled,
                            })
                          }
                        >
                          {schedule.enabled ? (
                            <Pause className="size-4" />
                          ) : (
                            <Play className="size-4" />
                          )}
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          aria-label="Edit schedule"
                          onClick={() => {
                            setEditing(schedule)
                            setEditForm(formFromSchedule(schedule))
                          }}
                        >
                          <Pencil className="size-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          aria-label="Delete schedule"
                          disabled={deleteSchedule.isPending}
                          onClick={() => deleteSchedule.mutate(schedule.id)}
                        >
                          <Trash2 className="size-4" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </CardContent>

      <Dialog open={editing !== null} onOpenChange={(open) => !open && setEditing(null)}>
        <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>Edit schedule</DialogTitle>
            <DialogDescription>
              Changes take effect on the next scheduler tick.
            </DialogDescription>
          </DialogHeader>
          <form className="space-y-4" onSubmit={handleEdit}>
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="edit-schedule-name">Name</Label>
                <Input
                  id="edit-schedule-name"
                  value={editForm.name}
                  onChange={(event) =>
                    setEditForm({ ...editForm, name: event.target.value })
                  }
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="edit-schedule-cron">Cron</Label>
                <Input
                  id="edit-schedule-cron"
                  className="font-mono text-xs"
                  value={editForm.cron}
                  onChange={(event) =>
                    setEditForm({ ...editForm, cron: event.target.value })
                  }
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="edit-schedule-timezone">Timezone</Label>
                <Input
                  id="edit-schedule-timezone"
                  value={editForm.timezone}
                  onChange={(event) =>
                    setEditForm({ ...editForm, timezone: event.target.value })
                  }
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="edit-schedule-job-type">Job type</Label>
                <Input
                  id="edit-schedule-job-type"
                  className="font-mono text-xs"
                  value={editForm.jobType}
                  onChange={(event) =>
                    setEditForm({ ...editForm, jobType: event.target.value })
                  }
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="edit-schedule-queue">Queue</Label>
                <Input
                  id="edit-schedule-queue"
                  value={editForm.queueName}
                  onChange={(event) =>
                    setEditForm({ ...editForm, queueName: event.target.value })
                  }
                />
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div className="space-y-2">
                  <Label htmlFor="edit-schedule-priority">Priority</Label>
                  <Input
                    id="edit-schedule-priority"
                    type="number"
                    min={0}
                    max={1000}
                    value={editForm.priority}
                    onChange={(event) =>
                      setEditForm({ ...editForm, priority: event.target.value })
                    }
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="edit-schedule-attempts">Max attempts</Label>
                  <Input
                    id="edit-schedule-attempts"
                    type="number"
                    min={0}
                    max={100}
                    value={editForm.maxAttempts}
                    onChange={(event) =>
                      setEditForm({ ...editForm, maxAttempts: event.target.value })
                    }
                  />
                </div>
              </div>
            </div>
            <div className="space-y-2">
              <Label htmlFor="edit-schedule-payload">Job payload (JSON)</Label>
              <textarea
                id="edit-schedule-payload"
                rows={8}
                className="w-full rounded-md border bg-background px-3 py-2 font-mono text-xs"
                value={editForm.payload}
                onChange={(event) =>
                  setEditForm({ ...editForm, payload: event.target.value })
                }
              />
            </div>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setEditing(null)}>
                Cancel
              </Button>
              <Button type="submit" disabled={updateSchedule.isPending}>
                {updateSchedule.isPending ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : null}
                Save changes
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </Card>
  )
}
