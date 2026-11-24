import { type ClassValue, clsx } from 'clsx'
import { extendTailwindMerge } from 'tailwind-merge'

// Register custom typography scale tokens from globals.css (@theme --text-caption / --text-body)
// as font-size utilities so tailwind-merge does not treat them as text colors and strip
// text-primary-foreground / text-destructive-foreground on sized buttons and badges.
const twMerge = extendTailwindMerge({
  extend: {
    theme: {
      text: ['caption', 'body'],
    },
  },
})

/** Merge class names, letting later Tailwind utilities win over earlier ones. */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
