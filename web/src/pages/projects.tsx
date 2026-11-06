import { useState, type FormEvent } from "react"
import { Link } from "react-router-dom"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { ArrowUpRight, FolderKanban, Loader2, Plus } from "lucide-react"
import { toast } from "sonner"
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
import { api, ApiError } from "@/lib/api"
import { useAuth } from "@/lib/auth"
import { formatRelative } from "@/lib/format"

export function ProjectsPage() {
  const { token } = useAuth()
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [name, setName] = useState("")

  const projectsQuery = useQuery({
    queryKey: ["projects"],
    queryFn: () => api.listProjects(token!),
    enabled: Boolean(token),
  })

  const createProject = useMutation({
    mutationFn: (projectName: string) => api.createProject(token!, projectName),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["projects"] })
      setOpen(false)
      setName("")
      toast.success("Project created")
    },
    onError: (error) => {
      toast.error(error instanceof ApiError ? error.message : "Could not create project")
    },
  })

  function handleCreate(event: FormEvent) {
    event.preventDefault()
    const trimmed = name.trim()
    if (!trimmed) return
    createProject.mutate(trimmed)
  }

  const projects = projectsQuery.data?.projects ?? []

  return (
    <div className="space-y-8">
      <div className="flex flex-col justify-between gap-4 sm:flex-row sm:items-end">
        <div>
          <p className="text-sm font-medium text-muted-foreground">Workspace</p>
          <h1 className="mt-1 text-3xl font-semibold tracking-tight">Projects</h1>
          <p className="mt-2 max-w-xl text-sm leading-6 text-muted-foreground">
            Applications connected to RelayForge. Each project owns its API keys,
            queues, jobs, and workers.
          </p>
        </div>
        <Button onClick={() => setOpen(true)}>
          <Plus className="size-4" />
          New project
        </Button>
      </div>

      {projectsQuery.isError && (
        <div className="rounded-lg border border-destructive/30 bg-destructive/10 px-4 py-3 text-sm text-destructive">
          {projectsQuery.error instanceof ApiError
            ? projectsQuery.error.message
            : "Could not load projects"}
        </div>
      )}

      {projectsQuery.isLoading ? (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {[0, 1, 2].map((item) => (
            <Skeleton key={item} className="h-44 rounded-xl" />
          ))}
        </div>
      ) : projects.length === 0 ? (
        <Card className="border-dashed">
          <CardContent className="flex flex-col items-center justify-center px-6 py-16 text-center">
            <span className="grid size-12 place-items-center rounded-2xl bg-muted">
              <FolderKanban className="size-5 text-muted-foreground" />
            </span>
            <h2 className="mt-4 text-lg font-medium">Create your first project</h2>
            <p className="mt-1 max-w-sm text-sm text-muted-foreground">
              Projects isolate jobs and API keys. Start with an application name.
            </p>
            <Button className="mt-5" onClick={() => setOpen(true)}>
              <Plus className="size-4" />
              New project
            </Button>
          </CardContent>
        </Card>
      ) : (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {projects.map((project) => (
            <Link key={project.id} to={`/projects/${project.id}`} className="group">
              <Card className="h-full transition-all duration-200 group-hover:-translate-y-0.5 group-hover:ring-foreground/20">
                <CardHeader>
                  <div className="flex items-start justify-between gap-4">
                    <span className="grid size-10 place-items-center rounded-xl bg-primary/10 text-primary">
                      <FolderKanban className="size-5" />
                    </span>
                    <ArrowUpRight className="size-4 text-muted-foreground transition-transform group-hover:-translate-y-0.5 group-hover:translate-x-0.5" />
                  </div>
                  <CardTitle className="mt-3 text-base">{project.name}</CardTitle>
                  <CardDescription>
                    Created {formatRelative(project.created_at)}
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  <p className="font-mono text-[11px] text-muted-foreground">
                    {project.id}
                  </p>
                </CardContent>
              </Card>
            </Link>
          ))}
        </div>
      )}

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <form onSubmit={handleCreate}>
            <DialogHeader>
              <DialogTitle>Create project</DialogTitle>
              <DialogDescription>
                Give your application a recognizable name.
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-2 py-5">
              <Label htmlFor="project-name">Project name</Label>
              <Input
                id="project-name"
                placeholder="My E-commerce App"
                value={name}
                onChange={(event) => setName(event.target.value)}
                maxLength={100}
                autoFocus
                required
              />
            </div>
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => setOpen(false)}
              >
                Cancel
              </Button>
              <Button type="submit" disabled={createProject.isPending}>
                {createProject.isPending && <Loader2 className="size-4 animate-spin" />}
                Create project
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  )
}
