import { type ClassValue, clsx } from 'clsx'
import { twMerge } from 'tailwind-merge'

/** Merge conditional class names (shadcn-vue convention). */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
