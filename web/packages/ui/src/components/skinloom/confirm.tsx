'use client'

import { createContext, useCallback, useContext, useRef, useState } from 'react'
import type * as React from 'react'

import { Input } from '../ui/input'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '../ui/alert-dialog'
import { fill, useUiLabels } from './labels'

export interface ConfirmOptions {
  /** A question naming the thing: 删除角色「Steve」？ */
  title: React.ReactNode
  /** What happens, and what cannot be undone. */
  description?: React.ReactNode
  /** The action, not "OK": 删除、注销账号. */
  confirmLabel: string
  cancelLabel?: string
  /** Red confirm button; the default for confirm(), which is for things that cannot be undone. */
  destructive?: boolean
  /** For the most serious actions: the user must type this (an email, a name) first. */
  requireText?: string
}

type Ask = (options: ConfirmOptions) => Promise<boolean>

const ConfirmContext = createContext<Ask | null>(null)

/**
 * Lets any component ask before an irreversible action, with an AlertDialog instead of
 * window.confirm (web/design.md §10.2). Mount once near the root; plugins' confirm()
 * uses the same dialog.
 */
export function ConfirmProvider({ children }: { children: React.ReactNode }) {
  const [options, setOptions] = useState<ConfirmOptions | null>(null)
  const [open, setOpen] = useState(false)
  const [typed, setTyped] = useState('')
  const resolver = useRef<((ok: boolean) => void) | null>(null)

  const ask = useCallback<Ask>((o) => {
    resolver.current?.(false) // a second question replaces an unanswered one
    setOptions(o)
    setTyped('')
    setOpen(true)
    return new Promise<boolean>((resolve) => {
      resolver.current = resolve
    })
  }, [])

  const answer = (ok: boolean) => {
    resolver.current?.(ok)
    resolver.current = null
    setOpen(false)
  }

  return (
    <ConfirmContext.Provider value={ask}>
      {children}
      <AlertDialog open={open} onOpenChange={(next) => !next && answer(false)}>
        {options && <ConfirmBody options={options} typed={typed} setTyped={setTyped} answer={answer} />}
      </AlertDialog>
    </ConfirmContext.Provider>
  )
}

function ConfirmBody({
  options,
  typed,
  setTyped,
  answer,
}: {
  options: ConfirmOptions
  typed: string
  setTyped: (s: string) => void
  answer: (ok: boolean) => void
}) {
  const t = useUiLabels()
  const blocked = options.requireText !== undefined && typed.trim() !== options.requireText
  return (
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>{options.title}</AlertDialogTitle>
        {options.description && <AlertDialogDescription>{options.description}</AlertDialogDescription>}
      </AlertDialogHeader>
      {options.requireText !== undefined && (
        <label className="flex flex-col gap-2 text-sm">
          <span className="text-muted-foreground">{fill(t.typeToConfirm, { text: options.requireText })}</span>
          <Input value={typed} onChange={(e) => setTyped(e.target.value)} autoComplete="off" spellCheck={false} />
        </label>
      )}
      <AlertDialogFooter>
        <AlertDialogCancel>{options.cancelLabel ?? t.cancel}</AlertDialogCancel>
        <AlertDialogAction variant={options.destructive === false ? 'default' : 'destructive'} disabled={blocked} onClick={() => answer(true)}>
          {options.confirmLabel}
        </AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  )
}

/** `await confirm({...})` is true only when the user chose the action. */
export function useConfirm(): Ask {
  const ask = useContext(ConfirmContext)
  if (!ask) throw new Error('useConfirm outside <ConfirmProvider>')
  return ask
}
