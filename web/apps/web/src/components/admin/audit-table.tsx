'use client'

import { type AuditPage, type AuditView, listAudit } from '@skinloom/api-client'
import {
  Alert,
  AlertAction,
  AlertDescription,
  Button,
  CodeBlock,
  Empty,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
  Input,
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
  Popover,
  PopoverContent,
  PopoverTrigger,
  RelativeTime,
  Skeleton,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@skinloom/ui'
import { CircleAlert, ScrollText, Search } from 'lucide-react'
import { useTranslations } from 'next-intl'
import { type FormEvent, useCallback, useEffect, useRef, useState } from 'react'

/** Who did what, newest first; filtered by an action prefix such as auth.login or admin., and optional actor ID. */
export function AuditTable() {
  const t = useTranslations('admin')
  const tc = useTranslations('common')
  const tRef = useRef(t)
  tRef.current = t
  const [actionInput, setActionInput] = useState('')
  const [actorInput, setActorInput] = useState('')
  const [action, setAction] = useState('')
  const [actor, setActor] = useState<number | undefined>(undefined)
  const [records, setRecords] = useState<AuditView[] | null>(null)
  const [next, setNext] = useState(0)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const latest = useRef(0)
  const abortRef = useRef<AbortController | null>(null)
  const lastBeforeRef = useRef<number | undefined>(undefined)

  const load = useCallback(
    async (before?: number) => {
      abortRef.current?.abort()
      const controller = new AbortController()
      abortRef.current = controller
      const seq = ++latest.current
      lastBeforeRef.current = before
      setLoading(true)
      setError(null)
      try {
        const page = (
          await listAudit(
            { limit: 50, ...(action ? { action } : {}), ...(actor ? { actor } : {}), ...(before ? { before } : {}) },
            { signal: controller.signal },
          )
        ).data as AuditPage
        if (seq !== latest.current) return
        setRecords((prev) => (before && prev ? [...prev, ...page.records] : page.records))
        setNext(page.next)
      } catch {
        if (!controller.signal.aborted && seq === latest.current) setError(tRef.current('errors.load'))
      } finally {
        if (seq === latest.current) setLoading(false)
      }
    },
    [action, actor],
  )
  useEffect(() => {
    void load()
    return () => abortRef.current?.abort()
  }, [load])

  function onFilter(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    setAction(actionInput.trim())
    const parsedActor = Number(actorInput.trim())
    setActor(actorInput.trim() !== '' && Number.isInteger(parsedActor) && parsedActor > 0 ? parsedActor : undefined)
  }

  return (
    <div className="flex flex-col gap-4">
      <form onSubmit={onFilter} role="search" className="flex flex-wrap items-center gap-2">
        <InputGroup className="w-full sm:w-72">
          <InputGroupAddon>
            <Search aria-hidden />
          </InputGroupAddon>
          <InputGroupInput
            type="search"
            name="action"
            value={actionInput}
            onChange={(e) => {
              setActionInput(e.target.value)
              if (e.target.value === '' && action !== '') setAction('')
            }}
            placeholder={t('actionHint')}
            aria-label={t('filter')}
            className="font-mono"
          />
        </InputGroup>
        <Input
          type="number"
          name="actor"
          min={1}
          value={actorInput}
          onChange={(e) => {
            setActorInput(e.target.value)
            if (e.target.value === '' && actor !== undefined) setActor(undefined)
          }}
          placeholder={t('actorHint')}
          aria-label={t('actorHint')}
          className="w-full tabular-nums sm:w-40"
        />
        <Button type="submit" variant="outline" size="sm" className="pointer-coarse:h-10">
          {t('filter')}
        </Button>
      </form>
      {error && (
        <Alert variant="destructive" role="alert">
          <CircleAlert />
          <AlertDescription>{error}</AlertDescription>
          <AlertAction>
            <Button type="button" variant="outline" size="sm" onClick={() => void load(lastBeforeRef.current)}>
              {tc('retry')}
            </Button>
          </AlertAction>
        </Alert>
      )}
      {records === null && !error && <Skeleton className="h-64 rounded-xl" />}
      {records && records.length === 0 && (
        <Empty className="border border-dashed py-16">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <ScrollText />
            </EmptyMedia>
            <EmptyTitle>{t('emptyAudit')}</EmptyTitle>
          </EmptyHeader>
        </Empty>
      )}
      {records && records.length > 0 && (
        // Table itself owns the horizontal scroll container; first column stays pinned on narrow screens.
        <div className="overflow-hidden rounded-xl border bg-card">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="sticky left-0 z-10 bg-card pl-4">{t('time')}</TableHead>
                <TableHead>{t('action')}</TableHead>
                <TableHead>{t('actor')}</TableHead>
                <TableHead>{t('target')}</TableHead>
                <TableHead>IP</TableHead>
                <TableHead>{t('details')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {records.map((r) => {
                const rawMeta = r.meta ? JSON.stringify(r.meta) : ''
                return (
                  <TableRow key={r.id}>
                    <TableCell className="sticky left-0 z-10 bg-card pl-4 text-caption whitespace-nowrap text-muted-foreground">
                      <RelativeTime date={r.createdAt} />
                    </TableCell>
                    <TableCell className="font-mono text-caption">{r.action}</TableCell>
                    <TableCell className="text-caption tabular-nums">
                      {r.actorId ? `#${r.actorId}` : <span className="text-muted-foreground">{t('system')}</span>}
                    </TableCell>
                    <TableCell className="text-caption">
                      {r.targetType} <span className="font-mono">{r.targetId}</span>
                    </TableCell>
                    <TableCell className="font-mono text-caption text-muted-foreground">{r.ip}</TableCell>
                    <TableCell className="max-w-md font-mono text-caption text-muted-foreground">
                      {rawMeta ? (
                        <Popover>
                          <PopoverTrigger
                            render={
                              <button
                                type="button"
                                className="max-w-full cursor-pointer truncate text-left underline-offset-4 hover:text-foreground hover:underline focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
                              />
                            }
                          >
                            {rawMeta}
                          </PopoverTrigger>
                          <PopoverContent align="end" className="w-96 max-w-[90vw] p-3">
                            <CodeBlock code={JSON.stringify(r.meta, null, 2)} />
                          </PopoverContent>
                        </Popover>
                      ) : null}
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </div>
      )}
      {next > 0 && (
        <Button variant="outline" className="self-center" loading={loading} onClick={() => void load(next)}>
          {t('more')}
        </Button>
      )}
    </div>
  )
}
