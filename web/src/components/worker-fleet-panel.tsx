import { useQuery } from "@tanstack/react-query"
import { Activity, ServerOff, Wifi } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import {
  Card,
  CardContent,
  CardDescription,
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
import { api, ApiError, type WorkerSummary } from "@/lib/api"
import { useAuth } from "@/lib/auth"
import { formatRelative } from "@/lib/format"

export function WorkerFleetPanel({ projectId }: { projectId: string }) {
  const { token } = useAuth()
  const workersQuery = useQuery({
    queryKey: ["workers", projectId],
    queryFn: () => api.listWorkers(token!, projectId),
    enabled: Boolean(token),
    refetchInterval: 5000,
  })

  const workers = workersQuery.data?.workers ?? []
  const online = workers.filter((worker) => worker.status === "online").length

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Activity className="size-4" />
          Workers
        </CardTitle>
        <CardDescription>
          {online} online · {workers.length} registered. Workers refresh every
          five seconds.
        </CardDescription>
      </CardHeader>
      <CardContent className="p-0">
        {workersQuery.isLoading ? (
          <div className="space-y-2 p-4">
            <Skeleton className="h-10 rounded-lg" />
            <Skeleton className="h-10 rounded-lg" />
          </div>
        ) : workersQuery.isError ? (
          <p className="p-4 text-sm text-destructive">
            {workersQuery.error instanceof ApiError
              ? workersQuery.error.message
              : "Could not load workers"}
          </p>
        ) : workers.length === 0 ? (
          <div className="flex flex-col items-center justify-center px-6 py-10 text-center">
            <ServerOff className="size-7 text-muted-foreground/60" />
            <p className="mt-2 text-sm font-medium">No workers registered</p>
            <p className="mt-1 text-sm text-muted-foreground">
              Create a worker pool and issue an enrollment token to connect one.
            </p>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Worker</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Running</TableHead>
                  <TableHead>Heartbeat</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {workers.map((worker) => (
                  <WorkerRow key={worker.id} worker={worker} />
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function WorkerRow({ worker }: { worker: WorkerSummary }) {
  const online = worker.status === "online"
  return (
    <TableRow>
      <TableCell>
        <p className="text-sm font-medium">{worker.name}</p>
        <p className="mt-0.5 font-mono text-[11px] text-muted-foreground">
          {worker.mode} · {worker.id.slice(0, 8)}
        </p>
      </TableCell>
      <TableCell>
        <Badge variant={online ? "default" : "secondary"}>
          <Wifi className="mr-1 size-3" />
          {online ? "Online" : "Offline"}
        </Badge>
      </TableCell>
      <TableCell className="tabular-nums text-muted-foreground">
        {worker.running_jobs}
      </TableCell>
      <TableCell className="whitespace-nowrap text-muted-foreground">
        {worker.last_heartbeat_at
          ? formatRelative(worker.last_heartbeat_at)
          : "Never"}
      </TableCell>
    </TableRow>
  )
}
