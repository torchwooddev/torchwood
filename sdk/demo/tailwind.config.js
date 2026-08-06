/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{js,ts,jsx,tsx}"],
  theme: {
    extend: {
      fontFamily: {
        sans: ["DM Sans", "system-ui", "sans-serif"],
        mono: ["JetBrains Mono", "monospace"],
      },
      colors: {
        Torchwood: {
          bg: "#0b1220",
          panel: "#111827",
          card: "#151f33",
          border: "#243049",
          muted: "#8fa3bf",
          accent: "#22d3ee",
          accent2: "#38bdf8",
          success: "#34d399",
          danger: "#f87171",
        },
      },
      boxShadow: {
        glow: "0 0 40px rgba(34, 211, 238, 0.12)",
      },
    },
  },
  plugins: [],
};
