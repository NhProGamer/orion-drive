import type { Config } from 'tailwindcss'
import animate from 'tailwindcss-animate'

// The color scale maps Tailwind/shadcn semantic tokens onto the Nebula design
// system CSS variables defined in src/assets/styles/tokens.css.
export default {
  darkMode: ['selector', '[data-theme="dark"]'],
  content: ['./index.html', './src/**/*.{vue,ts,tsx}'],
  theme: {
    extend: {
      colors: {
        border: 'var(--border)',
        input: 'var(--border)',
        ring: 'var(--accent)',
        background: 'var(--bg-0)',
        foreground: 'var(--fg-0)',
        primary: { DEFAULT: 'var(--accent)', foreground: 'var(--bg-0)' },
        secondary: { DEFAULT: 'var(--bg-2)', foreground: 'var(--fg-0)' },
        muted: { DEFAULT: 'var(--bg-2)', foreground: 'var(--fg-2)' },
        accent: { DEFAULT: 'var(--accent-bg)', foreground: 'var(--accent-fg)' },
        destructive: { DEFAULT: 'var(--danger)', foreground: 'var(--fg-0)' },
        card: { DEFAULT: 'var(--bg-1)', foreground: 'var(--fg-0)' },
        popover: { DEFAULT: 'var(--bg-1)', foreground: 'var(--fg-0)' },
      },
      borderRadius: {
        lg: 'var(--r-lg)',
        md: 'var(--r-md)',
        sm: 'var(--r-sm)',
      },
      fontFamily: {
        sans: 'var(--font-sans)',
        mono: 'var(--font-mono)',
        display: 'var(--font-display)',
      },
      keyframes: {
        'fade-in': {
          from: { opacity: '0', transform: 'translateY(4px)' },
          to: { opacity: '1', transform: 'translateY(0)' },
        },
      },
      animation: {
        'fade-in': 'fade-in 120ms cubic-bezier(0.2,0,0,1) both',
      },
    },
  },
  plugins: [animate],
} satisfies Config
