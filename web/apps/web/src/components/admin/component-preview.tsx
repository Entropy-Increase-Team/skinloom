'use client'

import type { TextureView } from '@skinloom/api-client'
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger, Alert, AlertDescription, AlertTitle, Badge, Button, ButtonGroup, ButtonLink, Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle, Checkbox, CodeBlock, CopyField, Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger, Drawer, DrawerContent, DrawerDescription, DrawerHeader, DrawerTitle, DrawerTrigger, DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger, Dropzone, Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle, Field, FieldDescription, FieldError, FieldGroup, FieldLabel, Input, InputGroup, InputGroupAddon, InputGroupInput, Kbd, KbdGroup, Label, MenuIcon, menuIcons, PageHeader, PixelBackdrop, PlayerCard, Popover, PopoverContent, PopoverDescription, PopoverHeader, PopoverTitle, PopoverTrigger, Progress, RadioGroup, RadioGroupItem, RelativeTime, SaveBar, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle, SheetTrigger, Skeleton, SkinCarousel, SkinViewer, Slider, Spinner, StatCard, StatusDot, Switch, Table, TableBody, TableCell, TableHead, TableHeader, TableRow, Tabs, TabsContent, TabsList, TabsTrigger, Textarea, TextureCard, TexturePicker, TexturePreview, toast, ToggleGroup, ToggleGroupItem, Tooltip, TooltipContent, TooltipTrigger, UsageMeter, useConfirm, UserAvatar } from '@skinloom/ui'
import { Coins, Ellipsis, Pencil, Plus, Search, Shirt, Trash2, Upload } from 'lucide-react'
import Link from 'next/link'
import { useTranslations } from 'next-intl'
import { useTheme } from 'next-themes'
import { useState } from 'react'
import type * as React from 'react'

/** Tokens a theme sets (web/design.md §2.2), shown as swatches. */
const COLOR_TOKENS = [
  'background',
  'card',
  'muted',
  'foreground',
  'muted-foreground',
  'faint-foreground',
  'border',
  'input',
  'primary',
  'primary-foreground',
  'highlight',
  'highlight-foreground',
  'success',
  'success-foreground',
  'destructive',
  'destructive-foreground',
  'overlay',
  'ring',
  'chart-1',
  'chart-2',
  'chart-3',
  'chart-4',
  'chart-5',
  'pixel-a',
  'pixel-b',
]

const TYPE_SCALE: [string, string, string][] = [
  ['text-5xl font-semibold', '48 / 600', 'headline'],
  ['text-3xl font-medium', '30 / 500', 'headline'],
  ['text-2xl font-medium', '24 / 500', 'headline'],
  ['text-lg font-medium', '18 / 500', 'headline'],
  ['text-base font-medium', '16 / 500', 'headline'],
  ['text-body', '15 / 26', 'body'],
  ['text-sm', '14 / 22', 'body'],
  ['text-caption text-muted-foreground', '13 / 20', 'body'],
  ['text-xs text-faint-foreground', '12 / 16', 'body'],
]

function Section({ id, title, children }: { id: string; title: string; children: React.ReactNode }) {
  return (
    <section id={id} aria-labelledby={`${id}-title`} className="flex scroll-mt-20 flex-col gap-5 border-t pt-8">
      <h2 id={`${id}-title`} className="text-lg font-medium">
        {title}
      </h2>
      {children}
    </section>
  )
}

/** A labelled example inside a section. */
function Demo({ name, children, className }: { name: string; children: React.ReactNode; className?: string }) {
  return (
    <div className="flex min-w-0 flex-col gap-3">
      <span className="font-mono text-xs text-faint-foreground">{name}</span>
      <div className={className ?? 'flex flex-wrap items-center gap-3'}>{children}</div>
    </div>
  )
}

export function ComponentPreview({ textures }: { textures: TextureView[] }) {
  const t = useTranslations('componentPreview')
  const s = (k: string, v?: Record<string, string>) => t(`sample.${k}` as 'sample.headline', v)
  const { resolvedTheme, setTheme } = useTheme()
  const confirm = useConfirm()
  const [picker, setPicker] = useState(false)
  const [picked, setPicked] = useState<number | null>(null)
  const [nickname, setNickname] = useState('dnyo666')
  const [saving, setSaving] = useState(false)
  const skins = textures.filter((x) => x.type !== 'cape')
  const first = skins[0]
  const modelLabel = (type: string) => (type === 'alex' ? s('slim') : s('classic'))

  return (
    <div className="flex flex-col gap-10">
      <PageHeader
        title={t('title')}
        description={t('description')}
        actions={
          <ToggleGroup
            aria-label={t('mode')}
            variant="outline"
            value={[resolvedTheme === 'dark' ? 'dark' : 'light']}
            onValueChange={(v) => v[0] && setTheme(v[0])}
          >
            <ToggleGroupItem value="light">{t('light')}</ToggleGroupItem>
            <ToggleGroupItem value="dark">{t('dark')}</ToggleGroupItem>
          </ToggleGroup>
        }
      />

      <nav className="flex flex-wrap gap-x-4 gap-y-2 text-sm text-muted-foreground">
        {(['colors', 'type', 'buttons', 'forms', 'feedback', 'overlays', 'display', 'skins', 'patterns', 'icons'] as const).map((k) => (
          <a key={k} href={`#${k}`} className="hover:text-foreground">
            {t(`sections.${k}`)}
          </a>
        ))}
      </nav>

      <Section id="colors" title={t('sections.colors')}>
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
          {COLOR_TOKENS.map((name) => (
            <div key={name} className="flex items-center gap-3">
              <span className="size-10 shrink-0 rounded-md border" style={{ background: `var(--${name})` }} />
              <span className="min-w-0 truncate font-mono text-xs">--{name}</span>
            </div>
          ))}
        </div>
      </Section>

      <Section id="type" title={t('sections.type')}>
        <div className="flex flex-col gap-4">
          {TYPE_SCALE.map(([cls, size, kind]) => (
            <div key={cls} className="grid grid-cols-[6rem_1fr] items-baseline gap-4">
              <span className="font-mono text-xs text-faint-foreground">{size}</span>
              <span className={`${cls} truncate`}>{kind === 'headline' ? s('headline') : s('body')}</span>
            </div>
          ))}
          <div className="grid grid-cols-[6rem_1fr] items-baseline gap-4">
            <span className="font-mono text-xs text-faint-foreground">mono</span>
            <span className="font-mono text-caption">e3b0c442-98fc-1c14-9afb-f4c8996fb924 · 1,240 · 0123456789</span>
          </div>
        </div>
      </Section>

      <Section id="buttons" title={t('sections.buttons')}>
        <Demo name="variant">
          <Button>{s('newPlayer')}</Button>
          <Button variant="secondary">{s('dress')}</Button>
          <Button variant="outline">
            <Upload />
            {s('dress')}
          </Button>
          <Button variant="ghost">{s('rename')}</Button>
          <Button variant="destructive">{s('delete')}</Button>
          <Button variant="destructive-soft">{s('delete')}</Button>
          <Button variant="link">{s('browse')}</Button>
        </Demo>
        <Demo name="size · loading · icon">
          <Button size="lg">{s('newPlayer')}</Button>
          <Button>{s('newPlayer')}</Button>
          <Button size="sm">{s('newPlayer')}</Button>
          <Button size="xs">{s('newPlayer')}</Button>
          <Button loading>{s('newPlayer')}</Button>
          <Button size="icon" variant="outline" aria-label={s('rename')}>
            <Pencil />
          </Button>
          <Button size="icon-sm" variant="ghost" aria-label={s('menu')}>
            <Ellipsis />
          </Button>
          <ButtonLink  variant="outline" render={<Link href="/skinlib" />}>
            {s('browse')}
          </ButtonLink>
        </Demo>
        <Demo name="ButtonGroup">
          <ButtonGroup>
            <Button variant="outline">{s('rename')}</Button>
            <Button variant="outline">{s('wear')}</Button>
            <Button variant="outline" size="icon" aria-label={s('delete')}>
              <Trash2 />
            </Button>
          </ButtonGroup>
        </Demo>
      </Section>

      <Section id="forms" title={t('sections.forms')}>
        <div className="grid gap-8 lg:grid-cols-2">
          <FieldGroup className="max-w-md">
            <Field>
              <FieldLabel htmlFor="cp-email">{s('email')}</FieldLabel>
              <Input id="cp-email" type="email" placeholder="steve@example.cn" />
              <FieldDescription>{s('emailHint')}</FieldDescription>
            </Field>
            <Field data-invalid>
              <FieldLabel htmlFor="cp-nick">{s('nickname')}</FieldLabel>
              <Input id="cp-nick" aria-invalid value={nickname} onChange={(e) => setNickname(e.target.value)} />
              <FieldError>{s('invalid')}</FieldError>
            </Field>
            <Field>
              <FieldLabel htmlFor="cp-bio">{s('bio')}</FieldLabel>
              <Textarea id="cp-bio" rows={3} />
            </Field>
            <InputGroup>
              <InputGroupAddon>
                <Search />
              </InputGroupAddon>
              <InputGroupInput placeholder={t('sections.forms')} aria-label="search" />
            </InputGroup>
          </FieldGroup>
          <div className="flex max-w-md flex-col gap-6">
            <Field>
              <FieldLabel>{s('model')}</FieldLabel>
              <Select
                items={[
                  { value: 'steve', label: s('classic') },
                  { value: 'alex', label: s('slim') },
                ]}
                defaultValue="steve"
              >
                <SelectTrigger className="w-48">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="steve">{s('classic')}</SelectItem>
                  <SelectItem value="alex">{s('slim')}</SelectItem>
                </SelectContent>
              </Select>
            </Field>
            <RadioGroup defaultValue="steve" aria-label={s('model')}>
              {(['steve', 'alex'] as const).map((v) => (
                <Label key={v} className="gap-2 font-normal">
                  <RadioGroupItem value={v} />
                  {modelLabel(v)}
                </Label>
              ))}
            </RadioGroup>
            <Label className="gap-2 font-normal">
              <Checkbox defaultChecked />
              {s('remember')}
            </Label>
            <Label className="gap-3 font-normal">
              <Switch defaultChecked />
              {s('public')}
            </Label>
            <Field>
              <FieldLabel>{s('volume')}</FieldLabel>
              <Slider defaultValue={[60]} max={100} className="max-w-60" />
            </Field>
            <ToggleGroup defaultValue={['all']} variant="outline">
              <ToggleGroupItem value="all">{s('skin')}</ToggleGroupItem>
              <ToggleGroupItem value="cape">{s('cape')}</ToggleGroupItem>
            </ToggleGroup>
          </div>
        </div>
      </Section>

      <Section id="feedback" title={t('sections.feedback')}>
        <div className="grid gap-4 lg:grid-cols-2">
          <Alert>
            <AlertTitle>{s('alertTitle')}</AlertTitle>
            <AlertDescription>{s('alertBody')}</AlertDescription>
          </Alert>
          <Alert variant="destructive">
            <AlertTitle>{s('errorTitle')}</AlertTitle>
            <AlertDescription>{s('errorBody')}</AlertDescription>
          </Alert>
        </div>
        <Demo name="Badge">
          <Badge>{s('public')}</Badge>
          <Badge variant="secondary">{s('classic')}</Badge>
          <Badge variant="outline">{s('slim')}</Badge>
          <Badge variant="success">{s('ok')}</Badge>
          <Badge variant="warning">{s('warn')}</Badge>
          <Badge variant="destructive">{s('error')}</Badge>
        </Demo>
        <Demo name="Progress · Skeleton · Spinner" className="flex flex-col gap-4">
          <Progress value={62} className="max-w-md" />
          <div className="flex items-center gap-3">
            <Skeleton className="size-10 rounded-md" />
            <div className="flex flex-col gap-2">
              <Skeleton className="h-4 w-48" />
              <Skeleton className="h-3 w-32" />
            </div>
            <Spinner className="ml-4" />
          </div>
        </Demo>
        <Demo name="Toast · useConfirm">
          <Button variant="outline" onClick={() => toast.success(s('toast'))}>
            {s('showToast')}
          </Button>
          <Button
            variant="destructive-soft"
            onClick={async () => {
              if (await confirm({ title: s('confirmTitle'), description: s('confirmBody'), confirmLabel: s('confirmAction') })) toast.success(s('toast'))
            }}
          >
            {s('askConfirm')}
          </Button>
          <Button
            variant="destructive-soft"
            onClick={() => confirm({ title: s('typedTitle'), description: s('typedBody'), confirmLabel: s('typedAction'), requireText: 'steve@example.cn' })}
          >
            {s('askTyped')}
          </Button>
        </Demo>
        <Empty className="border">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <Shirt />
            </EmptyMedia>
            <EmptyTitle>{s('emptyTitle')}</EmptyTitle>
            <EmptyDescription>{s('emptyBody')}</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <ButtonLink variant="outline" render={<Link href="/skinlib" />}>
              {s('browse')}
            </ButtonLink>
          </EmptyContent>
        </Empty>
      </Section>

      <Section id="overlays" title={t('sections.overlays')}>
        <Demo name="Dialog · Sheet · Drawer · Popover · DropdownMenu · Tooltip">
          <Dialog>
            <DialogTrigger render={<Button variant="outline" />}>{s('dialog')}</DialogTrigger>
            <DialogContent>
              <DialogHeader>
                <DialogTitle>{s('rename')}</DialogTitle>
                <DialogDescription>{s('dialogBody')}</DialogDescription>
              </DialogHeader>
              <Input defaultValue="Steve" aria-label={s('nickname')} />
              <DialogFooter>
                <Button>{s('rename')}</Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
          <Sheet>
            <SheetTrigger render={<Button variant="outline" />}>{s('sheet')}</SheetTrigger>
            <SheetContent>
              <SheetHeader>
                <SheetTitle>{s('sheet')}</SheetTitle>
                <SheetDescription>{s('dialogBody')}</SheetDescription>
              </SheetHeader>
            </SheetContent>
          </Sheet>
          <Drawer>
            <DrawerTrigger render={<Button variant="outline" />}>{s('drawer')}</DrawerTrigger>
            <DrawerContent>
              <DrawerHeader>
                <DrawerTitle>{s('drawer')}</DrawerTitle>
                <DrawerDescription>{s('dialogBody')}</DrawerDescription>
              </DrawerHeader>
            </DrawerContent>
          </Drawer>
          <Popover>
            <PopoverTrigger render={<Button variant="outline" />}>{s('popover')}</PopoverTrigger>
            <PopoverContent>
              <PopoverHeader>
                <PopoverTitle>{s('question')}</PopoverTitle>
                <PopoverDescription>{s('answer')}</PopoverDescription>
              </PopoverHeader>
            </PopoverContent>
          </Popover>
          <DropdownMenu>
            <DropdownMenuTrigger render={<Button variant="outline" />}>{s('menu')}</DropdownMenuTrigger>
            <DropdownMenuContent>
              <DropdownMenuItem>
                <Shirt />
                {s('wear')}
              </DropdownMenuItem>
              <DropdownMenuItem>
                <Pencil />
                {s('rename')}
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem variant="destructive">
                <Trash2 />
                {s('delete')}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
          <Tooltip>
            <TooltipTrigger render={<Button variant="outline" size="icon" aria-label={s('tooltip')} />}>
              <Plus />
            </TooltipTrigger>
            <TooltipContent>{s('tooltip')}</TooltipContent>
          </Tooltip>
          <KbdGroup>
            <Kbd>Ctrl</Kbd>
            <Kbd>K</Kbd>
          </KbdGroup>
        </Demo>
      </Section>

      <Section id="display" title={t('sections.display')}>
        <div className="grid gap-6 lg:grid-cols-2">
          <Card>
            <CardHeader>
              <CardTitle>{s('alertTitle')}</CardTitle>
              <CardDescription>{s('emailHint')}</CardDescription>
            </CardHeader>
            <CardContent className="text-body">{s('body')}</CardContent>
            <CardFooter className="justify-end gap-2">
              <Button variant="ghost">{s('rename')}</Button>
              <Button>{s('newPlayer')}</Button>
            </CardFooter>
          </Card>
          <Tabs defaultValue="a">
            <TabsList>
              <TabsTrigger value="a">{s('tab1')}</TabsTrigger>
              <TabsTrigger value="b">{s('tab2')}</TabsTrigger>
            </TabsList>
            <TabsContent value="a">
              <Accordion>
                <AccordionItem value="q">
                  <AccordionTrigger>{s('question')}</AccordionTrigger>
                  <AccordionContent>{s('answer')}</AccordionContent>
                </AccordionItem>
              </Accordion>
            </TabsContent>
            <TabsContent value="b">
              <CopyField value="slp_3f9c2e7a51b84d06a2c1" />
            </TabsContent>
          </Tabs>
        </div>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{s('nickname')}</TableHead>
              <TableHead>{s('players')}</TableHead>
              <TableHead>{t('sections.feedback')}</TableHead>
              <TableHead className="text-right">{s('score')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {[
              ['momo_', 1, 'ok', 120],
              ['Lin', 2, 'warn', 1240],
              ['steelwing', 3, 'error', 36],
            ].map(([name, players, status, score]) => (
              <TableRow key={name}>
                <TableCell className="font-medium">{name}</TableCell>
                <TableCell className="tabular-nums">{players}</TableCell>
                <TableCell>
                  <StatusDot status={status as 'ok'}>{s(status as string)}</StatusDot>
                </TableCell>
                <TableCell className="text-right tabular-nums">{Number(score).toLocaleString()}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Section>

      <Section id="skins" title={t('sections.skins')}>
        {textures.length === 0 ? (
          <p className="text-sm text-muted-foreground">{s('noTextures')}</p>
        ) : (
          <>
            <Demo name="TextureCard · comfortable" className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-5">
              {textures.slice(0, 5).map((x) => (
                <TextureCard
                  key={x.id}
                  linkAs={Link}
                  href={`/skinlib/show/${x.id}`}
                  texture={{ ...x, uploader: x.uploader ? { name: x.uploader.nickname, userId: x.uploader.id } : null }}
                />
              ))}
            </Demo>
            <Demo name="TextureCard · compact" className="grid grid-cols-3 gap-3 sm:grid-cols-4 lg:grid-cols-8">
              {textures.map((x) => (
                <TextureCard
                  key={x.id}
                  linkAs={Link}
                  density="compact"
                  href={`/skinlib/show/${x.id}`}
                  texture={{ ...x, uploader: x.uploader ? { name: x.uploader.nickname, userId: x.uploader.id } : null }}
                />
              ))}
            </Demo>
            {first && (
              <div className="grid gap-6 lg:grid-cols-[1fr_20rem]">
                <Demo name="SkinCarousel" className="block">
                  <SkinCarousel
                    label={s('carousel')}
                    linkAs={Link}
                    className="h-[30rem]"
                    items={skins.map((x) => ({
                      id: x.id,
                      name: x.name,
                      skin: `/textures/${x.hash}`,
                      model: x.type === 'alex' ? 'slim' : 'default',
                      href: `/skinlib/show/${x.id}`,
                      uploader: x.uploader ? { name: x.uploader.nickname, userId: x.uploader.id } : undefined,
                      likes: x.likes,
                      modelLabel: modelLabel(x.type),
                    }))}
                  />
                </Demo>
                <Demo name="SkinViewer · PixelBackdrop" className="block">
                  <PixelBackdrop className="h-[30rem] rounded-2xl border">
                    <SkinViewer skin={`/textures/${first.hash}`} model={first.type === 'alex' ? 'slim' : 'default'} label={first.name} />
                  </PixelBackdrop>
                </Demo>
              </div>
            )}
            <div className="grid gap-6 sm:grid-cols-2 lg:grid-cols-4">
              {first && (
                <PlayerCard
                  name="dnyo666"
                  uuid="e3b0c442-98fc-1c14-9afb-f4c8996fb924"
                  skin={{ id: first.id, type: first.type === 'alex' ? 'alex' : 'steve' }}
                  details={[
                    { label: s('skin'), value: first.name },
                    { label: s('cape'), value: '—' },
                  ]}
                  actions={
                    <>
                      <Button variant="outline" size="sm" onClick={() => setPicker(true)}>
                        <Shirt />
                        {s('dress')}
                      </Button>
                      <span className="flex-1" />
                      <Button variant="ghost" size="icon-sm" aria-label={s('menu')}>
                        <Ellipsis />
                      </Button>
                    </>
                  }
                />
              )}
              <Demo name="TexturePreview · UserAvatar" className="flex flex-col items-start gap-4">
                <PixelBackdrop className="h-40 w-full">
                  <TexturePreview texture={{ id: textures[0].id }} type={textures[0].type} height={120} />
                </PixelBackdrop>
                <div className="flex items-end gap-3">
                  {([16, 24, 32, 48, 64] as const).map((size) => (
                    <UserAvatar key={size} of={{ texture: textures[0].id }} size={size} />
                  ))}
                  <UserAvatar of={{ texture: textures[0].id }} size={64} cube />
                </div>
                {picked !== null && <span className="text-caption text-muted-foreground">{s('picked', { name: textures.find((x) => x.id === picked)?.name ?? '' })}</span>}
              </Demo>
            </div>
            <TexturePicker
              open={picker}
              onOpenChange={setPicker}
              title={s('pick')}
              items={textures}
              selectedId={picked}
              onPick={(x) => {
                setPicked(x.id)
                setPicker(false)
              }}
            />
          </>
        )}
      </Section>

      <Section id="patterns" title={t('sections.patterns')}>
        <div className="rounded-xl border p-5">
          <PageHeader
            title={s('pageTitle')}
            description={s('pageDesc')}
            actions={
              <Button>
                <Plus />
                {s('newPlayer')}
              </Button>
            }
          />
        </div>
        <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
          <StatCard label={s('score')} value="1,240" note={s('weekly')} icon={<Coins className="size-3.5 text-highlight" aria-hidden />} />
          <StatCard label={s('players')} value="3" note={s('weekly')} />
          <div className="col-span-2 flex flex-col justify-center gap-4 rounded-xl border bg-card p-5">
            <UsageMeter label={s('storage')} value={2.4} max={10} format={(n) => `${n} MB`} />
            <UsageMeter label={s('storage')} value={8.6} max={10} format={(n) => `${n} MB`} />
          </div>
        </div>
        <Demo name="StatusDot · RelativeTime">
          <StatusDot status="ok">{s('ok')}</StatusDot>
          <StatusDot status="warn">{s('warn')}</StatusDot>
          <StatusDot status="error">{s('error')}</StatusDot>
          <StatusDot status="off">{s('off')}</StatusDot>
          <RelativeTime date={new Date(Date.now() - 12 * 60_000)} className="text-caption text-muted-foreground" />
          <RelativeTime date={new Date(Date.now() - 26 * 3600_000)} className="text-caption text-muted-foreground" />
        </Demo>
        <div className="grid gap-6 lg:grid-cols-2">
          <Demo name="CopyField · CodeBlock" className="flex flex-col gap-3">
            <CopyField value="https://skin.example.cn/api/yggdrasil" />
            <CodeBlock code={'skinloom theme new my-site --extends default\nskinloom theme check data/themes/my-site'} />
          </Demo>
          <Demo name="Dropzone" className="block">
            <Dropzone accept="image/png" hint={s('dropHintLine')} onFile={(f) => toast(f.name)} />
          </Demo>
        </div>
        <Demo name="SaveBar" className="flex flex-col gap-3">
          <span className="text-caption text-muted-foreground">{s('saveBarHint')}</span>
          <Input className="max-w-xs" value={nickname} onChange={(e) => setNickname(e.target.value)} aria-label={s('nickname')} />
          <SaveBar
            count={nickname === 'dnyo666' ? 0 : 1}
            saving={saving}
            onDiscard={() => setNickname('dnyo666')}
            onSave={() => {
              setSaving(true)
              setTimeout(() => {
                setSaving(false)
                setNickname('dnyo666')
                toast.success(s('toast'))
              }, 800)
            }}
          />
        </Demo>
      </Section>

      <Section id="icons" title={t('sections.icons')}>
        <div className="grid grid-cols-2 gap-x-6 gap-y-2 sm:grid-cols-3 lg:grid-cols-5">
          {Object.keys(menuIcons).map((name) => (
            <div key={name} className="flex h-8 items-center gap-2.5 text-muted-foreground">
              <MenuIcon name={name} />
              <span className="font-mono text-xs">{name}</span>
            </div>
          ))}
        </div>
      </Section>
    </div>
  )
}
