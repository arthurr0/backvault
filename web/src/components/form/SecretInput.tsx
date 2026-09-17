import { useState } from 'react'
import { Eye, EyeOff, RotateCcw } from 'lucide-react'
import { SECRET_MASK } from '@/api/types'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'

export interface SecretInputProps {
  id?: string
  value: string
  onChange: (value: string) => void
  placeholder?: string
  storedMask?: boolean
  autoComplete?: string
}

export function SecretInput({
  id,
  value,
  onChange,
  placeholder,
  storedMask,
  autoComplete = 'new-password',
}: SecretInputProps) {
  const [revealed, setRevealed] = useState(false)
  const masked = storedMask && value === SECRET_MASK

  if (masked) {
    return (
      <div className="flex items-center gap-2">
        <Input value={SECRET_MASK} readOnly id={id} className="font-mono" aria-label="Stored secret" />
        <Button size="sm" icon={<RotateCcw className="size-3.5" />} onClick={() => onChange('')}>
          Replace
        </Button>
      </div>
    )
  }

  return (
    <div className="relative flex items-center">
      <Input
        id={id}
        type={revealed ? 'text' : 'password'}
        value={value}
        autoComplete={autoComplete}
        spellCheck={false}
        placeholder={placeholder}
        onChange={(event) => onChange(event.target.value)}
        className="pr-10 font-mono"
      />
      <button
        type="button"
        aria-label={revealed ? 'Hide value' : 'Reveal value'}
        title={revealed ? 'Hide value' : 'Reveal value'}
        onClick={() => setRevealed((v) => !v)}
        className="absolute right-2 rounded p-1 text-soft hover:text-text"
      >
        {revealed ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
      </button>
    </div>
  )
}
