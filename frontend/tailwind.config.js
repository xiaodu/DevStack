/** @type {import('tailwindcss').Config} */
export default {
  content: ['./src/**/*.{svelte,js}'],
  daisyui: {
    themes: ['light', 'corporate', 'dim', 'night', 'business', 'dark'],
  },
  plugins: [require('daisyui')],
}