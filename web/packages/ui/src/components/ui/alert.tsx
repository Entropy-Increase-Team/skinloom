import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"
import { cn } from "#lib/utils"

const alertVariants = cva(
  "group/alert relative grid w-full gap-0.5 rounded-lg border px-2.5 py-2 text-left text-sm not-has-data-[slot=alert-title]:items-center has-data-[slot=alert-action]:grid-cols-[minmax(0,1fr)_auto] has-data-[slot=alert-action]:gap-x-2 has-[>svg]:grid-cols-[auto_minmax(0,1fr)] has-[>svg]:gap-x-2 has-[>svg]:has-data-[slot=alert-action]:grid-cols-[auto_minmax(0,1fr)_auto] has-data-[slot=alert-title]:*:[svg]:row-span-2 has-data-[slot=alert-title]:*:[svg]:self-start has-data-[slot=alert-title]:*:[svg]:translate-y-0.5 *:[svg]:text-current *:[svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default: "bg-card text-card-foreground",
        destructive:
          "border-destructive/30 bg-destructive/10 text-destructive *:data-[slot=alert-description]:text-destructive *:[svg]:text-current",
        warning:
          "border-warning/40 border-l-2 border-l-warning bg-warning/10 text-foreground *:[svg]:text-warning-foreground dark:*:[svg]:text-warning",
        success:
          "border-success/30 bg-success/10 text-success *:data-[slot=alert-description]:text-success *:[svg]:text-current",
        info:
          "border-primary/30 bg-primary/10 text-primary *:data-[slot=alert-description]:text-primary *:[svg]:text-current",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
)

function Alert({
  className,
  variant,
  ...props
}: React.ComponentProps<"div"> & VariantProps<typeof alertVariants>) {
  return (
    <div
      data-slot="alert"
      role="alert"
      className={cn(alertVariants({ variant }), className)}
      {...props}
    />
  )
}

function AlertTitle({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="alert-title"
      className={cn(
        "min-w-0 [overflow-wrap:anywhere] font-medium group-has-[>svg]/alert:col-start-2 [&_a]:underline [&_a]:underline-offset-3 [&_a]:hover:text-foreground",
        className
      )}
      {...props}
    />
  )
}

function AlertDescription({
  className,
  ...props
}: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="alert-description"
      className={cn(
        "min-w-0 [overflow-wrap:anywhere] text-sm text-balance text-muted-foreground group-has-[>svg]/alert:col-start-2 md:text-pretty [&_a]:underline [&_a]:underline-offset-3 [&_a]:hover:text-foreground [&_p:not(:last-child)]:mb-4",
        className
      )}
      {...props}
    />
  )
}

function AlertAction({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="alert-action"
      className={cn(
        "col-start-2 row-start-1 self-center justify-self-end group-has-[>svg]/alert:col-start-3 group-has-data-[slot=alert-title]/alert:row-span-2",
        className
      )}
      {...props}
    />
  )
}

export { Alert, AlertTitle, AlertDescription, AlertAction }
