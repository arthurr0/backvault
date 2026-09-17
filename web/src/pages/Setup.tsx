import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router'
import { errorMessage, fieldErrors } from '@/api/client'
import { useSetup } from '@/api/hooks'
import { Button } from '@/components/ui/Button'
import { FieldShell } from '@/components/ui/Field'
import { Input } from '@/components/ui/Input'
import { passphraseStrength } from '@/lib/utils'
import { AuthShell } from './AuthShell'

export function SetupPage() {
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const setup = useSetup()
  const navigate = useNavigate()
  const strength = passphraseStrength(password)
  const mismatch = confirm.length > 0 && confirm !== password

  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (mismatch) return
    setup.mutate({ name, email, password }, { onSuccess: () => navigate('/', { replace: true }) })
  }

  const errors = fieldErrors(setup.error)

  return (
    <AuthShell
      title="Create the first administrator"
      description="This account manages jobs, sources, destinations and users."
      footer="You can add more users later in Settings."
    >
      <form className="flex flex-col gap-4" onSubmit={submit}>
        <FieldShell label="Name" htmlFor="setup-name" required error={errors.name}>
          <Input
            id="setup-name"
            autoFocus
            required
            autoComplete="name"
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </FieldShell>
        <FieldShell label="Email" htmlFor="setup-email" required error={errors.email}>
          <Input
            id="setup-email"
            type="email"
            required
            autoComplete="username"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
          />
        </FieldShell>
        <FieldShell
          label="Password"
          htmlFor="setup-password"
          required
          error={errors.password}
          help={password ? `${strength.label}. ${strength.hint}` : 'At least 10 characters'}
        >
          <Input
            id="setup-password"
            type="password"
            required
            minLength={8}
            autoComplete="new-password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
          />
        </FieldShell>
        <FieldShell
          label="Confirm password"
          htmlFor="setup-confirm"
          required
          error={mismatch ? 'The passwords do not match' : undefined}
        >
          <Input
            id="setup-confirm"
            type="password"
            required
            autoComplete="new-password"
            value={confirm}
            onChange={(event) => setConfirm(event.target.value)}
          />
        </FieldShell>
        {setup.isError ? (
          <p role="alert" className="rounded-lg bg-danger/10 px-3 py-2 text-sm text-danger">
            {errorMessage(setup.error)}
          </p>
        ) : null}
        <Button type="submit" variant="primary" loading={setup.isPending} className="w-full">
          Create account
        </Button>
      </form>
    </AuthShell>
  )
}
