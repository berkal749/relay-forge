import { useState, type FormEvent } from "react"
import { Link, useNavigate, useSearchParams } from "react-router-dom"
import { useQuery } from "@tanstack/react-query"
import { ArrowRight, Loader2, LockKeyhole, Mail, ShieldCheck, Ticket } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { api, ApiError } from "@/lib/api"
import { useAuth } from "@/lib/auth"
import { AuthLayout, ErrorBox } from "@/pages/login"

export function RegisterPage() {
  const { register } = useAuth()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [confirm, setConfirm] = useState("")
  const [inviteToken, setInviteToken] = useState(searchParams.get("invite") ?? "")
  const [error, setError] = useState("")
  const [submitting, setSubmitting] = useState(false)

  const configQuery = useQuery({
    queryKey: ["auth-config"],
    queryFn: () => api.authConfig(),
    staleTime: 60_000,
  })
  const registrationMode = configQuery.data?.registration_mode ?? "open"
  const registrationClosed = registrationMode === "closed"
  const inviteRequired = registrationMode === "invite"

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setError("")

    if (password.length < 8) {
      setError("Password must be at least 8 characters.")
      return
    }
    if (password !== confirm) {
      setError("Passwords do not match.")
      return
    }
    if (inviteRequired && !inviteToken.trim()) {
      setError("An invite token is required.")
      return
    }

    setSubmitting(true)
    try {
      await register(email, password, inviteRequired ? inviteToken.trim() : undefined)
      navigate("/", { replace: true })
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Unable to create account")
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <AuthLayout
      title="Create your account"
      description="Set up a workspace and start forging reliable jobs."
    >
      <form className="space-y-5" onSubmit={handleSubmit}>
        {error && <ErrorBox message={error} />}
        {registrationClosed && (
          <ErrorBox message="Registration is closed. Ask an administrator for an invite." />
        )}
        {inviteRequired && (
          <div className="space-y-2">
            <Label htmlFor="invite">Invite token</Label>
            <div className="relative">
              <Ticket className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                id="invite"
                placeholder="rf_invite_..."
                className="pl-9 font-mono text-xs"
                value={inviteToken}
                onChange={(event) => setInviteToken(event.target.value)}
                required
              />
            </div>
          </div>
        )}

        <div className="space-y-2">
          <Label htmlFor="email">Email</Label>
          <div className="relative">
            <Mail className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              id="email"
              type="email"
              autoComplete="email"
              placeholder="you@example.com"
              className="pl-9"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
              required
            />
          </div>
        </div>

        <div className="space-y-2">
          <Label htmlFor="password">Password</Label>
          <div className="relative">
            <LockKeyhole className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              id="password"
              type="password"
              autoComplete="new-password"
              placeholder="At least 8 characters"
              className="pl-9"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              minLength={8}
              maxLength={72}
              required
            />
          </div>
        </div>

        <div className="space-y-2">
          <Label htmlFor="confirm">Confirm password</Label>
          <div className="relative">
            <ShieldCheck className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              id="confirm"
              type="password"
              autoComplete="new-password"
              placeholder="Repeat your password"
              className="pl-9"
              value={confirm}
              onChange={(event) => setConfirm(event.target.value)}
              minLength={8}
              maxLength={72}
              required
            />
          </div>
        </div>

        <Button
          type="submit"
          className="h-9 w-full"
          disabled={submitting || registrationClosed || configQuery.isLoading}
        >
          {submitting ? <Loader2 className="size-4 animate-spin" /> : null}
          Create account
          {!submitting && <ArrowRight className="size-4" />}
        </Button>
      </form>

      <p className="mt-6 text-center text-sm text-muted-foreground">
        Already have an account?{" "}
        <Link to="/login" className="font-medium text-foreground underline-offset-4 hover:underline">
          Sign in
        </Link>
      </p>
    </AuthLayout>
  )
}
