import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"

const styles: Record<string, string> = {
  pending: "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300",
  scheduled: "border-sky-500/30 bg-sky-500/10 text-sky-700 dark:text-sky-300",
  running: "border-violet-500/30 bg-violet-500/10 text-violet-700 dark:text-violet-300",
  succeeded: "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
  failed: "border-destructive/30 bg-destructive/10 text-destructive",
  cancelled: "border-border bg-muted text-muted-foreground",
}

const dots: Record<string, string> = {
  pending: "bg-amber-500",
  scheduled: "bg-sky-500",
  running: "bg-violet-500",
  succeeded: "bg-emerald-500",
  failed: "bg-destructive",
  cancelled: "bg-muted-foreground",
}

export function StatusBadge({ status }: { status: string }) {
  return (
    <Badge
      variant="outline"
      className={cn("gap-1.5 capitalize", styles[status] ?? "")}
    >
      <span
        className={cn(
          "size-1.5 rounded-full",
          dots[status] ?? "bg-muted-foreground",
          status === "running" && "animate-pulse",
        )}
      />
      {status}
    </Badge>
  )
}
