import { Alert, AlertDescription, FieldDescription, FieldError, FieldLabel, Field as UiField, Input } from '@skinloom/ui'
import { CircleAlert } from 'lucide-react'
import type * as React from 'react'

/** A labelled input with an optional hint and the server's validation message. */
export function Field({
  id,
  label,
  hint,
  error,
  ...input
}: React.ComponentProps<'input'> & { id: string; label: string; hint?: string; error?: string }) {
  const describedBy = [hint && `${id}-hint`, error && `${id}-error`].filter(Boolean).join(' ') || undefined
  return (
    <UiField data-invalid={error ? true : undefined}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input id={id} name={id} aria-invalid={error ? true : undefined} aria-describedby={describedBy} {...input} />
      {hint && !error && <FieldDescription id={`${id}-hint`}>{hint}</FieldDescription>}
      {error && <FieldError id={`${id}-error`}>{error}</FieldError>}
    </UiField>
  )
}

/** A form-level error (not tied to one field): a destructive alert. */
export function FormError({ message }: { message: string | null }) {
  if (!message) return null
  return (
    <Alert variant="destructive" role="alert">
      <CircleAlert />
      <AlertDescription>{message}</AlertDescription>
    </Alert>
  )
}
