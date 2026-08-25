module.exports = {
  plugins: {
    'tailwindcss/nesting': {}, // 这个就是启用嵌套语法的关键（内部调用 postcss-nested）
    tailwindcss: {},
    autoprefixer: {},
  },
};
