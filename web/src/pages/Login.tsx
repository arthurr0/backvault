import { useState, type FormEvent } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router'
import { errorMessage, fieldErrors } from '@/api/client'
import { useLogin } from '@/api/hooks'
import { Button } from '@/components/ui/Button'
import { FieldShell } from '@/components/ui/Field'
import { Input } from '@/components/ui/Input'
import { AuthShell } from './AuthShell'

export function AuthLinks() {
  return (
    <span className="flex items-center justify-center gap-2">
      <Link to="/docs" className="hover:text-text">
        Documentation
      </Link>
      <span aria-hidden="true">·</span>
      <a
        href="https://github.com/arthurr0/backvault"
        target="_blank"
        rel="noopener noreferrer"
        className="hover:text-text"
      >
        GitHub
      </a>
    </span>
  )
}

export function LoginPage() {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const login = useLogin()
  const navigate = useNavigate()
  const [params] = useSearchParams()

  const submit = (event: FormEvent) => {
    event.preventDefault()
    login.mutate(
      { email, password },
      {
        onSuccess: () => navigate(params.get('next') ?? '/', { replace: true }),
      },
    )
  }

  const errors = fieldErrors(login.error)

  return (
    <AuthShell title="Sign in" description="Every backup, accounted for." footer={<AuthLinks />}>
      <form className="flex flex-col gap-4" onSubmit={submit}>
        <FieldShell label="Email" htmlFor="login-email" required error={errors.email}>
          <Input
            id="login-email"
            type="email"
            autoComplete="username"
            autoFocus
            required
            value={email}
            onChange={(event) => setEmail(event.target.value)}
          />
        </FieldShell>
        <FieldShell label="Password" htmlFor="login-password" required error={errors.password}>
          <Input
            id="login-password"
            type="password"
            autoComplete="current-password"
            required
            value={password}
            onChange={(event) => setPassword(event.target.value)}
          />
        </FieldShell>
        {login.isError ? (
          <p role="alert" className="rounded-lg bg-danger/10 px-3 py-2 text-sm text-danger">
            {errorMessage(login.error)}
          </p>
        ) : null}
        <Button type="submit" variant="primary" loading={login.isPending} className="w-full">
          Sign in
        </Button>
      </form>
    </AuthShell>
  )
}
