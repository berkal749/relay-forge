import { Link } from "react-router-dom"
import { ArrowUpRight, Inbox } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { StatusBadge } from "@/components/status-badge"
import type { Job } from "@/lib/api"
import { formatRelative } from "@/lib/format"

export function JobTable({
  projectId,
  jobs,
}: {
  projectId: string
  jobs: Job[]
}) {
  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between">
        <div>
          <CardTitle>Recent jobs</CardTitle>
          <p className="mt-1 text-sm text-muted-foreground">
            Jobs refresh automatically every few seconds.
          </p>
        </div>
        <Badge variant="secondary">{jobs.length} jobs</Badge>
      </CardHeader>
      <CardContent className="p-0">
        {jobs.length === 0 ? (
          <div className="flex flex-col items-center justify-center px-6 py-14 text-center">
            <Inbox className="size-8 text-muted-foreground/60" />
            <p className="mt-3 text-sm font-medium">No jobs yet</p>
            <p className="mt-1 text-sm text-muted-foreground">
              Submit your first job to see it here.
            </p>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Job</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Attempts</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead className="w-12" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {jobs.map((job) => (
                  <TableRow key={job.id}>
                    <TableCell>
                      <Link
                        to={`/projects/${projectId}/jobs/${job.id}`}
                        className="font-medium hover:underline"
                      >
                        {job.job_type}
                      </Link>
                      <p className="mt-0.5 font-mono text-[11px] text-muted-foreground">
                        {job.queue_name} · {job.id.slice(0, 8)}
                      </p>
                    </TableCell>
                    <TableCell>
                      <StatusBadge status={job.status} />
                    </TableCell>
                    <TableCell className="tabular-nums text-muted-foreground">
                      {job.attempts}/{job.max_attempts}
                    </TableCell>
                    <TableCell className="whitespace-nowrap text-muted-foreground">
                      {formatRelative(job.created_at)}
                    </TableCell>
                    <TableCell>
                      <Link
                        to={`/projects/${projectId}/jobs/${job.id}`}
                        aria-label={`View job ${job.id}`}
                      >
                        <ArrowUpRight className="size-4 text-muted-foreground" />
                      </Link>
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
