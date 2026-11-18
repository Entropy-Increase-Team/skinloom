'use client'

import {
  completeSetupWizard,
  type DBConfigInput,
  type MailConfigInput,
  type MailConfigInputDriver,
  type MailConfigInputEncryption,
  type SetupState,
  type StorageConfigInput,
  type TestMailResult,
  testSetupMail,
} from '@skinloom/api-client'
import {
  Alert,
  AlertDescription,
  Button,
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
  Field as UiField,
  FieldLabel,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Separator,
  StatusDot,
} from '@skinloom/ui'
import { ChevronDown, Mail } from 'lucide-react'
import { useTranslations } from 'next-intl'
import { type FormEvent, useState } from 'react'

import { Field, FormError } from '@/components/auth/field'
import { problemOf } from '@/lib/problem'

import { setupErrorMessage } from './types'

const COMMON_TIMEZONES = [
  'Asia/Shanghai',
  'Asia/Tokyo',
  'Asia/Hong_Kong',
  'Asia/Taipei',
  'Asia/Singapore',
  'UTC',
  'Europe/London',
  'Europe/Berlin',
  'America/New_York',
  'America/Los_Angeles',
]

export function NewSiteStep({
  state,
  db,
  storage,
  trustedProxies,
  onBack,
  onCompleted,
}: {
  state: SetupState
  db: DBConfigInput
  storage: StorageConfigInput
  trustedProxies: string[]
  onBack: () => void
  onCompleted: (redirectTo: string) => void
}) {
  const t = useTranslations('setup')
  const tf = useTranslations('fieldErrors')

  const [siteName, setSiteName] = useState('')
  const [siteUrl, setSiteUrl] = useState(() =>
    typeof window !== 'undefined' ? state.suggestedSiteUrl || window.location.origin : state.suggestedSiteUrl || '',
  )
  const [timezone, setTimezone] = useState(state.defaultTimezone || 'Asia/Shanghai')
  const [email, setEmail] = useState('')
  const [nickname, setNickname] = useState('')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')

  const [mailOpen, setMailOpen] = useState(false)
  const [mail, setMail] = useState<MailConfigInput>({
    driver: 'none',
    port: 587,
    encryption: 'starttls',
  })
  const [testTo, setTestTo] = useState('')
  const [mailTesting, setMailTesting] = useState(false)
  const [mailProbe, setMailProbe] = useState<TestMailResult | null>(null)

  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [fields, setFields] = useState<Record<string, string>>({})

  async function runTestMail() {
    setMailTesting(true)
    setMailProbe(null)
    setError(null)
    try {
      const res = await testSetupMail({
        mail,
        to: testTo || email,
      })
      setMailProbe(res.data as TestMailResult)
    } catch (err) {
      setError(setupErrorMessage(err, t('fresh.mailTestFailed')))
    } finally {
      setMailTesting(false)
    }
  }

  async function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    if (password !== confirm) {
      setFields({ confirm: tf('passwordMismatch') })
      return
    }
    setPending(true)
    setError(null)
    setFields({})
    try {
      const res = await completeSetupWizard({
        database: state.envLocked.database ? { useEnv: true } : db,
        storage: state.envLocked.storage ? { useEnv: true } : storage,
        trustedProxies,
        siteName: siteName.trim() || undefined,
        siteUrl: siteUrl.trim(),
        timezone,
        email,
        nickname,
        password,
        mail: mail.driver && mail.driver !== 'none' ? mail : undefined,
      })
      const data = res.data as { redirectTo?: string }
      onCompleted(data.redirectTo || '/')
    } catch (err) {
      const p = problemOf(err)
      const mapped = Object.fromEntries(
        Object.entries(p.fields).map(([k, v]) => [k, tf.has(k) ? tf(k as 'email') : v]),
      )
      setFields(mapped)
      if (Object.keys(mapped).some((k) => k.startsWith('mail.'))) {
        setMailOpen(true)
      }
      if (p.status === 403) setError(t('errors.token'))
      else if (p.status === 409) setError(t('errors.done'))
      else if (p.status === 422) {
        const known = new Set(['siteName', 'siteUrl', 'email', 'nickname', 'password', 'mail.host', 'mail.smtp.host', 'mail.from'])
        const extra = Object.entries(mapped)
          .filter(([k]) => !known.has(k))
          .map(([, v]) => v)
        if (extra.length > 0) setError(extra.join('; '))
      } else {
        setError(setupErrorMessage(err, t('errors.failed')))
      }
      setPending(false)
    }
  }

  const timezones = COMMON_TIMEZONES.includes(timezone) ? COMMON_TIMEZONES : [timezone, ...COMMON_TIMEZONES]

  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-6" noValidate>
      <FormError message={error} />

      <fieldset className="flex flex-col gap-4">
        <legend className="mb-1 text-sm font-medium">{t('site')}</legend>
        <Field
          id="siteName"
          label={t('siteName')}
          value={siteName}
          placeholder={t('fresh.siteNamePlaceholder')}
          onChange={(e) => setSiteName(e.target.value)}
          maxLength={64}
          error={fields.siteName}
        />
        <Field
          id="siteUrl"
          label={t('siteUrl')}
          hint={t('siteUrlHint')}
          type="url"
          value={siteUrl}
          onChange={(e) => setSiteUrl(e.target.value)}
          error={fields.siteUrl}
        />
        <UiField>
          <FieldLabel>{t('fresh.timezone')}</FieldLabel>
          <Select value={timezone} onValueChange={(v) => setTimezone(v ?? 'Asia/Shanghai')}>
            <SelectTrigger className="w-full sm:w-72">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {timezones.map((tz) => (
                <SelectItem key={tz} value={tz}>
                  {tz}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </UiField>
      </fieldset>

      <Separator />

      <fieldset className="flex flex-col gap-4">
        <legend className="mb-1 text-sm font-medium">{t('admin')}</legend>
        <Field
          id="email"
          label={t('email')}
          type="email"
          autoComplete="username"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          required
          error={fields.email}
        />
        <Field
          id="nickname"
          label={t('nickname')}
          autoComplete="nickname"
          value={nickname}
          onChange={(e) => setNickname(e.target.value)}
          required
          maxLength={32}
          error={fields.nickname}
        />
        <Field
          id="password"
          label={t('password')}
          hint={t('passwordHint')}
          type="password"
          autoComplete="new-password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          required
          minLength={8}
          maxLength={128}
          error={fields.password}
        />
        <Field
          id="confirm"
          label={t('confirm')}
          type="password"
          autoComplete="new-password"
          value={confirm}
          onChange={(e) => setConfirm(e.target.value)}
          required
          error={fields.confirm}
        />
      </fieldset>

      <Collapsible open={mailOpen} onOpenChange={setMailOpen} className="rounded-xl border bg-card p-4">
        <CollapsibleTrigger className="flex w-full items-center justify-between gap-2 text-left text-sm font-medium">
          <span className="flex items-center gap-2">
            <Mail className="size-4 text-muted-foreground" aria-hidden />
            {t('fresh.mailTitle')}
          </span>
          <ChevronDown className={`size-4 text-muted-foreground transition-transform ${mailOpen ? 'rotate-180' : ''}`} aria-hidden />
        </CollapsibleTrigger>
        <CollapsibleContent className="flex flex-col gap-4 pt-4">
          <UiField>
            <FieldLabel>{t('fresh.mailDriver')}</FieldLabel>
            <Select
              value={mail.driver ?? 'none'}
              onValueChange={(v) => {
                setMailProbe(null)
                setMail({ ...mail, driver: (v ?? 'none') as MailConfigInputDriver })
              }}
            >
              <SelectTrigger className="w-full sm:w-64">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="none">{t('fresh.mailDrivers.none')}</SelectItem>
                <SelectItem value="smtp">SMTP</SelectItem>
                <SelectItem value="log">{t('fresh.mailDrivers.log')}</SelectItem>
              </SelectContent>
            </Select>
          </UiField>

          {mail.driver === 'smtp' && (
            <>
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <Field
                  id="mail-host"
                  label={t('fresh.mailHost')}
                  value={mail.host ?? ''}
                  onChange={(e) => setMail({ ...mail, host: e.target.value })}
                  error={fields['mail.host'] ?? fields['mail.smtp.host']}
                />
                <Field
                  id="mail-port"
                  label={t('fresh.mailPort')}
                  type="number"
                  value={mail.port ?? 587}
                  onChange={(e) => setMail({ ...mail, port: Number(e.target.value) || 0 })}
                />
                <UiField>
                  <FieldLabel>{t('fresh.mailEncryption')}</FieldLabel>
                  <Select
                    value={mail.encryption ?? 'starttls'}
                    onValueChange={(v) => setMail({ ...mail, encryption: (v ?? 'starttls') as MailConfigInputEncryption })}
                  >
                    <SelectTrigger className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="starttls">STARTTLS (587)</SelectItem>
                      <SelectItem value="tls">TLS / SSL (465)</SelectItem>
                      <SelectItem value="none">{t('fresh.mailPlain')}</SelectItem>
                    </SelectContent>
                  </Select>
                </UiField>
                <Field
                  id="mail-from"
                  label={t('fresh.mailFrom')}
                  placeholder="noreply@example.com"
                  value={mail.from ?? ''}
                  onChange={(e) => setMail({ ...mail, from: e.target.value })}
                  error={fields['mail.from']}
                />
                <Field
                  id="mail-user"
                  label={t('fresh.mailUser')}
                  value={mail.username ?? ''}
                  onChange={(e) => setMail({ ...mail, username: e.target.value })}
                />
                <Field
                  id="mail-pass"
                  label={t('fresh.mailPass')}
                  type="password"
                  autoComplete="new-password"
                  value={mail.password ?? ''}
                  onChange={(e) => setMail({ ...mail, password: e.target.value })}
                />
              </div>
              <div className="flex flex-col gap-2 sm:flex-row sm:items-end">
                <div className="flex-1">
                  <Field
                    id="mail-test-to"
                    label={t('fresh.mailTestTo')}
                    type="email"
                    placeholder={email || 'admin@example.com'}
                    value={testTo}
                    onChange={(e) => setTestTo(e.target.value)}
                  />
                </div>
                <Button type="button" variant="outline" loading={mailTesting} onClick={() => void runTestMail()}>
                  {t('fresh.mailTest')}
                </Button>
              </div>
              {mailProbe && (
                <Alert variant={mailProbe.ok ? 'default' : 'destructive'}>
                  <AlertDescription>
                    <StatusDot status={mailProbe.ok ? 'ok' : 'error'}>
                      {mailProbe.ok ? mailProbe.summary : mailProbe.errorDetail ?? mailProbe.summary}
                    </StatusDot>
                  </AlertDescription>
                </Alert>
              )}
            </>
          )}
        </CollapsibleContent>
      </Collapsible>

      <div className="flex flex-wrap items-center justify-between gap-3">
        <Button type="button" variant="outline" disabled={pending} onClick={onBack}>
          {t('infra.back')}
        </Button>
        <Button type="submit" loading={pending}>
          {t('submit')}
        </Button>
      </div>
    </form>
  )
}
