import { NavLink, Outlet, useNavigate } from "react-router-dom"
import { FolderKanban, LogOut, Ticket, Workflow } from "lucide-react"
import { Button } from "@/components/ui/button"
import { useAuth } from "@/lib/auth"
import { cn } from "@/lib/utils"

export function AppShell() {
  const { user, logout } = useAuth()
  const navigate = useNavigate()

  async function handleLogout() {
    await logout()
    navigate("/login", { replace: true })
  }

  return (
    <div className="min-h-svh bg-background">
      <header className="sticky top-0 z-40 border-b bg-background/85 backdrop-blur-xl">
        <div className="mx-auto flex h-16 max-w-7xl items-center justify-between gap-4 px-4 sm:px-6 lg:px-8">
          <div className="flex items-center gap-8">
            <button
              type="button"
              onClick={() => navigate("/")}
              className="flex items-center gap-2.5"
            >
              <span className="grid size-8 place-items-center rounded-lg bg-primary text-primary-foreground">
                <Workflow className="size-4" />
              </span>
              <span className="text-base font-semibold tracking-tight">
                RelayForge
              </span>
            </button>

            <NavLink
              to="/"
              className={({ isActive }) =>
                cn(
                  "hidden items-center gap-2 text-sm text-muted-foreground transition-colors hover:text-foreground sm:flex",
                  isActive && "text-foreground",
                )
              }
            >
              <FolderKanban className="size-4" />
              Projects
            </NavLink>

            {user?.role === "admin" && (
              <NavLink
                to="/admin/invites"
                className={({ isActive }) =>
                  cn(
                    "hidden items-center gap-2 text-sm text-muted-foreground transition-colors hover:text-foreground sm:flex",
                    isActive && "text-foreground",
                  )
                }
              >
                <Ticket className="size-4" />
                Invites
              </NavLink>
            )}
          </div>

          <div className="flex items-center gap-3">
            <span className="hidden max-w-56 truncate text-sm text-muted-foreground sm:block">
              {user?.email}
            </span>
            <Button variant="ghost" size="sm" onClick={handleLogout}>
              <LogOut className="size-4" />
              Sign out
            </Button>
          </div>
        </div>
      </header>

      <main className="mx-auto w-full max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
        <Outlet />
      </main>
    </div>
  )
}
