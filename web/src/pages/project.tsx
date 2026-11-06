import { useState, type FormEvent } from "react"
import { Link, useNavigate, useParams } from "react-router-dom"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  Activity,
  ArrowLeft,
  CheckCircle2,
  CircleDashed,
  Loader2,
  Pencil,
  Settings2,
  Trash2,
  XCircle,
} from "lucide-react"
import { toast } from "sonner"
import { ApiKeyPanel } from "@/components/api-key-panel"
import { JobTable } from "@/components/job-table"
import { ProjectSettingsDialog } from "@/components/project-settings-dialog"
import { SubmitJobForm } from "@/components/submit-job-form"
import { WorkerTokenPanel } from "@/components/worker-token-panel"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
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
import { api, ApiError } from "@/lib/api"
import { useAuth } from "@/lib/auth"
import { formatDate } from "@/lib/format"

export function ProjectPage() {
  const { projectId } = useParams()
  const { token } = useAuth()
  const queryClient = useQueryClient()
  const navigate = useNavigate()

  const [renameOpen, setRenameOpen] = useState(false)
  const [archiveOpen, setArchiveOpen] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [name, setName] = useState("")

  const projectQuery = useQuery({
    queryKey: ["project", projectId],
    queryFn: () => api.getProject(token!, projectId!),
    enabled: Boolean(token && projectId),
  })

  const jobsQuery = useQuery({
    queryKey: ["jobs", projectId],
    queryFn: () => api.listJobs(token!, projectId!),
    enabled: Boolean(token && projectId),
    refetchInterval: 3000,
  })

  const renameProject = useMutation({
    mutationFn: (nextName: string) =>
      api.updateProject(token!, projectId!, { name: nextName }),
    onSuccess: (result) => {
      queryClient.setQueryData(["project", projectId], result)
      queryClient.invalidateQueries({ queryKey: ["projects"] })
      setRenameOpen(false)
      toast.success("Project renamed")
    },
    onError: (error) => {
      toast.error(error instanceof ApiError ? error.message : "Could not rename project")
    },
  })

  const archiveProject = useMutation({
    mutationFn: () => api.deleteProject(token!, projectId!),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["projects"] })
      toast.success("Project archived")
      navigate("/", { replace: true })
    },
    onError: (error) => {
      toast.error(error instanceof ApiError ? error.message : "Could not archive project")
    },
  })

  if (projectQuery.isLoading) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-10 w-64" />
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          {[0, 1, 2, 3].map((item) => (
            <Skeleton key={item} className="h-28 rounded-xl" />
          ))}
        </div>
        <Skeleton className="h-80 rounded-xl" />
      </div>
    )
  }

  if (projectQuery.isError || !projectQuery.data) {
    return (
      <div className="space-y-4">
        <Link to="/" className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="size-4" />
          Projects
        </Link>
        <Card>
          <CardContent className="py-12 text-center">
            <p className="font-medium">Project not found</p>
            <p className="mt-1 text-sm text-muted-foreground">
              {projectQuery.error instanceof ApiError
                ? projectQuery.error.message
                : "This project may have been archived."}
            </p>
          </CardContent>
        </Card>
      </div>
    )
  }

  const project = projectQuery.data.project
  const jobs = jobsQuery.data?.jobs ?? []
  const active = jobs.filter((job) =>
    ["pending", "scheduled", "running"].includes(job.status),
  ).length
  const succeeded = jobs.filter((job) => job.status === "succeeded").length
  const failed = jobs.filter((job) => job.status === "failed").length

  function openRename() {
    setName(project.name)
    setRenameOpen(true)
  }

  function handleRename(event: FormEvent) {
    event.preventDefault()
    if (!name.trim()) return
    renameProject.mutate(name.trim())
  }

  return (
    <div className="space-y-8">
      <div>
        <Link to="/" className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="size-4" />
          Projects
        </Link>
        <div className="mt-4 flex flex-col justify-between gap-4 sm:flex-row sm:items-end">
          <div>
            <h1 className="text-3xl font-semibold tracking-tight">{project.name}</h1>
            <p className="mt-2 text-sm text-muted-foreground">
              Created {formatDate(project.created_at)} ·{" "}
              <span className="font-mono text-xs">{project.id}</span>
            </p>
          </div>
          <div className="flex gap-2">
            <Button variant="outline" onClick={openRename}>
              <Pencil className="size-4" />
              Rename
            </Button>
            <Button variant="outline" onClick={() => setSettingsOpen(true)}>
              <Settings2 className="size-4" />
              Settings
            </Button>
            <Button variant="destructive" onClick={() => setArchiveOpen(true)}>
              <Trash2 className="size-4" />
              Archive
            </Button>
          </div>
        </div>
      </div>

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard label="Total jobs" value={jobs.length} icon={CircleDashed} />
        <StatCard label="Active" value={active} icon={Activity} tone="violet" />
        <StatCard label="Succeeded" value={succeeded} icon={CheckCircle2} tone="emerald" />
        <StatCard label="Failed" value={failed} icon={XCircle} tone="destructive" />
      </div>

      <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_360px]">
        <div className="space-y-6">
          <SubmitJobForm
            key={`${project.default_queue_name}:${project.default_priority}:${project.default_max_attempts}`}
            projectId={project.id}
            defaults={{
              queue_name: project.default_queue_name,
              priority: project.default_priority,
              max_attempts: project.default_max_attempts,
            }}
          />
          {jobsQuery.isError ? (
            <Card>
              <CardContent className="py-8 text-sm text-destructive">
                {jobsQuery.error instanceof ApiError
                  ? jobsQuery.error.message
                  : "Could not load jobs"}
              </CardContent>
            </Card>
          ) : (
            <JobTable projectId={project.id} jobs={jobs} />
          )}
        </div>

        <div className="space-y-6">
          <ApiKeyPanel projectId={project.id} />
          <WorkerTokenPanel projectId={project.id} />
        </div>
      </div>

      <Dialog open={renameOpen} onOpenChange={setRenameOpen}>
        <DialogContent>
          <form onSubmit={handleRename}>
            <DialogHeader>
              <DialogTitle>Rename project</DialogTitle>
              <DialogDescription>Choose a new name for this application.</DialogDescription>
            </DialogHeader>
            <div className="space-y-2 py-5">
              <Label htmlFor="rename-project">Project name</Label>
              <Input
                id="rename-project"
                value={name}
                onChange={(event) => setName(event.target.value)}
                maxLength={100}
                autoFocus
                required
              />
            </div>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setRenameOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={renameProject.isPending}>
                {renameProject.isPending && <Loader2 className="size-4 animate-spin" />}
                Save changes
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <ProjectSettingsDialog
        project={project}
        open={settingsOpen}
        onOpenChange={setSettingsOpen}
      />

      <Dialog open={archiveOpen} onOpenChange={setArchiveOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Archive project?</DialogTitle>
            <DialogDescription>
              The project will be hidden, API keys stop working, and its job
              history is retained. This cannot be undone from the dashboard.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setArchiveOpen(false)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={() => archiveProject.mutate()}
              disabled={archiveProject.isPending}
            >
              {archiveProject.isPending && <Loader2 className="size-4 animate-spin" />}
              Archive project
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function StatCard({
  label,
  value,
  icon: Icon,
  tone = "default",
}: {
  label: string
  value: number
  icon: typeof Activity
  tone?: "default" | "violet" | "emerald" | "destructive"
}) {
  const toneClass = {
    default: "bg-muted text-muted-foreground",
    violet: "bg-violet-500/10 text-violet-600 dark:text-violet-300",
    emerald: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-300",
    destructive: "bg-destructive/10 text-destructive",
  }[tone]

  return (
    <Card>
      <CardContent className="flex items-center justify-between py-1">
        <div>
          <p className="text-sm text-muted-foreground">{label}</p>
          <p className="mt-1 text-2xl font-semibold tabular-nums">{value}</p>
        </div>
        <span className={`grid size-10 place-items-center rounded-xl ${toneClass}`}>
          <Icon className="size-5" />
        </span>
      </CardContent>
    </Card>
  )
}
