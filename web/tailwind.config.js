// Configuración de Tailwind para el front de Marga.
// Después de cambiar clases en pages/ o assets/js/, regenera el CSS:
//   cd web && npm install && npm run css
// (marga.css va en el repositorio: para compilar Go no hace falta Node).
module.exports = {
  darkMode: "class",
  content: ["./pages/*.html", "./assets/js/*.js", "./index.html"],
  theme: {
    extend: {
      colors: {
        "marga-bg": "#0A0A0A",
        "marga-surface": "#111111",
        "marga-card": "#171717",
        "marga-card-hover": "#1F1F1F",
        "marga-border": "#262626",
        "marga-border-subtle": "#1C1C1C",
        "marga-border-active": "#737373",
        "brand-purple": "#A3A3A3",
        "brand-violet": "#FAFAFA",
        "brand-indigo": "#737373",
        "brand-cyan": "#F5F5F5",
        "brand-blue": "#737373",
        "brand-sky": "#A3A3A3",
        "diff-add": "#737373",
        "diff-add-bg": "rgba(255, 255, 255, 0.05)",
        "diff-del": "#A3A3A3",
        "diff-del-bg": "rgba(255, 255, 255, 0.02)"
      },
      fontFamily: {
        sans: ["Geist", "Inter", "system-ui", "sans-serif"],
        mono: ["JetBrains Mono", "monospace"]
      }
    }
  },
  plugins: [require("@tailwindcss/forms"), require("@tailwindcss/container-queries")]
};
