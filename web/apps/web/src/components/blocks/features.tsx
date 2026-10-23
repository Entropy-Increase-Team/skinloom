import { Card, CardDescription, CardHeader, CardTitle } from '@skinloom/ui'
import { Gamepad2, Server, Shirt, Users } from 'lucide-react'
import { getTranslations } from 'next-intl/server'

const items = [
  { key: 'upload', Icon: Shirt },
  { key: 'players', Icon: Users },
  { key: 'launcher', Icon: Gamepad2 },
  { key: 'selfhost', Icon: Server },
] as const

export async function FeaturesBlock() {
  const t = await getTranslations('features')
  return (
    <section className="mx-auto max-w-6xl px-4 py-16">
      <h2 className="mb-8 text-2xl font-semibold tracking-tight">{t('title')}</h2>
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {items.map(({ key, Icon }) => (
          <Card key={key} className="gap-3">
            <CardHeader>
              <Icon className="mb-2 size-6 text-primary" aria-hidden />
              <CardTitle>{t(`${key}.title`)}</CardTitle>
              <CardDescription className="leading-relaxed">{t(`${key}.body`)}</CardDescription>
            </CardHeader>
          </Card>
        ))}
      </div>
    </section>
  )
}
