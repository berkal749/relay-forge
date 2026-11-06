import { Link, useParams } from "react-router-dom"
import { useQuery } from "@tanstack/react-query"
import {
  ArrowLeft,
  Clock3,
  RefreshCw,
  Radio,
  RotateCcw,
  Server,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { StatusBadge } from "@/components/status-badge"
import { api, ApiError } from "@/lib/api"
import { useAuth } from "@/lib/auth"
import { formatDate, formatRelative, isTerminal, prettyJson } from "@/lib/format"

export function JobPage() {
  const { projectId, jobId } = useParams()
  const { token } = useAuth()

  const jobQuery = useQuery({
    queryKey: ["job", projectId, jobId],
    queryFn: () => api.getJob(token!, projectId!, jobId!),
    enabled: Boolean(token && projectId && jobId),
    refetchInterval: (query) => {
      const job = query.state.data?.job
      return job && !isTerminal(job.status) ? 2000 : false
    },
  })

  const executionsQuery = useQuery({
    queryKey: ["executions", projectId, jobId],
    queryFn: () => api.listExecutions(token!, projectId!, jobId!),
    enabled: Boolean(token && projectId && jobId),
    refetchInterval: 2000,
  })

  const projectQuery = useQuery({
    queryKey: ["project", projectId],
    queryFn: () => api.getProject(token!, projectId!),
    enabled: Boolean(token && projectId),
  })

  if (jobQuery.isLoading) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-10 w-64" />
        <Skeleton className="h-32 rounded-xl" />
        <Skeleton className="h-64 rounded-xl" />
      </div>
    )
  }

  if (jobQuery.isError || !jobQuery.data) {
    return (
      <div className="space-y-4">
        <Link
          to={`/projects/${projectId}`}
          className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground"
        >
          <ArrowLeft className="size-4" />
          Back to project
        </Link>
        <Card>
          <CardContent className="py-12 text-center">
            <p className="font-medium">Job not found</p>
            <p className="mt-1 text-sm text-muted-foreground">
              {jobQuery.error instanceof ApiError
                ? jobQuery.error.message
                : "This job may no longer exist."}
            </p>
          </CardContent>
        </Card>
      </div>
    )
  }

  const job = jobQuery.data.job
  const executions = executionsQuery.data?.executions ?? []
  const live = !isTerminal(job.status)

  function refresh() {
    void jobQuery.refetch()
    void executionsQuery.refetch()
  }

  return (
    <div className="space-y-8">
      <div>
        <Link
          to={`/projects/${projectId}`}
          className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground"
        >
          <ArrowLeft className="size-4" />
          {projectQuery.data?.project.name ?? "Project"}
        </Link>
        <div className="mt-4 flex flex-col justify-between gap-4 sm:flex-row sm:items-start">
          <div>
            <div className="flex flex-wrap items-center gap-3">
              <h1 className="text-3xl font-semibold tracking-tight">{job.job_type}</h1>
              <StatusBadge status={job.status} />
              {live && (
                <span className="inline-flex items-center gap-1.5 text-xs font-medium text-emerald-600 dark:text-emerald-300">
                  <Radio className="size-3.5 animate-pulse" />
                  Live
                </span>
              )}
            </div>
            <p className="mt-2 text-sm text-muted-foreground">
              {job.queue_name} · <span className="font-mono text-xs">{job.id}</span>
            </p>
          </div>
          <Button variant="outline" onClick={refresh}>
            <RefreshCw className="size-4" />
            Refresh
          </Button>
        </div>
      </div>

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <SummaryCard
          icon={RotateCcw}
          label="Attempts"
          value={`${job.attempts} / ${job.max_attempts}`}
        />
        <SummaryCard icon={Server} label="Queue" value={job.queue_name} />
        <SummaryCard
          icon={Clock3}
          label="Scheduled"
          value={formatDate(job.scheduled_at)}
        />
        <SummaryCard
          icon={Clock3}
          label="Updated"
          value={formatRelative(job.updated_at)}
        />
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Payload</CardTitle>
        </CardHeader>
        <CardContent>
          <pre className="max-h-80 overflow-auto rounded-lg bg-muted/60 p-4 font-mono text-xs leading-6">
            {prettyJson(job.payload)}
          </pre>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex-row items-center justify-between">
          <div>
            <CardTitle>Execution history</CardTitle>
            <p className="mt-1 text-sm text-muted-foreground">
              Every attempt, result, and error for this job.
            </p>
          </div>
          <span className="text-sm text-muted-foreground">
            {executions.length} {executions.length === 1 ? "execution" : "executions"}
          </span>
        </CardHeader>
        <CardContent className="p-0">
          {executionsQuery.isLoading ? (
            <div className="space-y-2 p-4">
              <Skeleton className="h-12 rounded-lg" />
              <Skeleton className="h-12 rounded-lg" />
            </div>
          ) : executions.length === 0 ? (
            <p className="px-4 py-10 text-center text-sm text-muted-foreground">
              This job has not started executing yet.
            </p>
          ) : (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Attempt</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead>Started</TableHead>
                    <TableHead>Finished</TableHead>
                    <TableHead>Result / error</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {executions.map((execution) => (
                    <TableRow key={execution.id}>
                      <TableCell className="font-mono text-xs">
                        #{execution.attempt_number}
                      </TableCell>
                      <TableCell>
                        <StatusBadge status={execution.status} />
                      </TableCell>
                      <TableCell className="whitespace-nowrap text-sm text-muted-foreground">
                        {formatDate(execution.started_at)}
                      </TableCell>
                      <TableCell className="whitespace-nowrap text-sm text-muted-foreground">
                        {formatDate(execution.finished_at)}
                      </TableCell>
                      <TableCell className="max-w-md">
                        {execution.error_message ? (
                          <p className="text-sm text-destructive">
                            {execution.error_message}
                          </p>
                        ) : execution.result ? (
                          <pre className="max-h-24 overflow-auto font-mono text-[11px] leading-5 text-muted-foreground">
                            {prettyJson(execution.result)}
                          </pre>
                        ) : (
                          <span className="text-sm text-muted-foreground">—</span>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

function SummaryCard({
  icon: Icon,
  label,
  value,
}: {
  icon: typeof Clock3
  label: string
  value: string
}) {
  return (
    <Card>
      <CardContent className="py-1">
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Icon className="size-4" />
          {label}
        </div>
        <p className="mt-2 truncate text-sm font-medium">{value}</p>
      </CardContent>
    </Card>
  )
}
