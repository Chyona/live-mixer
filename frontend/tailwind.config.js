/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ['./src/**/*.{js,jsx,ts,tsx}'],
  theme: {
    extend: {
      keyframes: {
        pop: {
          '0%, 100%': { transform: 'scale(1)' },
          '50%': { transform: 'scale(1.15)' },
        },
        upFloat: {
          '0%, 100%': { transform: 'translateY(0)' },
          '50%': { transform: 'translateY(-5px)' },
        },
        boxShowBlue: {
          '0%': { 'box-shadow': '0 0 0 0 rgba(53, 107, 253, 0.4)' },
          '70%': { 'box-shadow': '0 0 0 12px rgba(53, 107, 253, 0)' },
          '100%': { 'box-shadow': '0 0 0 0 rgba(53, 107, 253, 0)' },
        }
      },
      animation: {
        pop: 'pop 1.5s ease-in-out infinite',
        upFloat: 'upFloat 1.5s ease-in-out infinite',
        boxShowBlue: 'boxShowBlue 1.5s ease-in-out infinite'
      }
    },
  },
  plugins: [],
  corePlugins: {
    preflight: false, // 禁用默认样式，避免与antd冲突
  },
};
