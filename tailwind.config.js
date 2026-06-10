/** @type {import('tailwindcss').Config} */
module.exports = {
  content: [
    "./web/templates/**/*.html",
    "./web/components/**/*.go",
    "./templates/**/*.html",
  ],
  darkMode: "class",
  theme: {
    extend: {
      colors: {
        surface: {
          lowest: "#09090b",
          low: "#0c0c0e",
          DEFAULT: "#09090b",
          container: "#18181b",
          high: "#27272a",
          highest: "#3f3f46",
        },
        primary: {
          DEFAULT: "#bec2ff",
          container: "#8a94ff",
        },
        "on-surface": {
          DEFAULT: "#fafafa",
          variant: "#a1a1aa",
          muted: "#71717a",
        },
        "on-primary": {
          container: "#18181b",
        },
        error: {
          DEFAULT: "#ef4444",
          soft: "rgba(239, 68, 68, 0.12)",
        },
        success: {
          DEFAULT: "#22c55e",
          soft: "rgba(34, 197, 94, 0.12)",
        },
        outline: {
          DEFAULT: "#52525b",
          variant: "#27272a",
        },
      },
      spacing: {
        xs: "4px",
        sm: "8px",
        md: "16px",
        lg: "24px",
        xl: "32px",
        "2xl": "48px",
        "3xl": "64px",
      },
      borderRadius: {
        sm: "4px",
        md: "6px",
        lg: "8px",
        xl: "12px",
      },
    },
  },
  plugins: [],
}