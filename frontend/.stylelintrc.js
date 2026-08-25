module.exports = {
  extends: ['stylelint-config-standard', 'stylelint-config-tailwindcss'],
  rules: {
    'selector-class-pattern': null,
    'keyframes-name-pattern': null,
    'color-function-notation': null,
    'alpha-value-notation': null,
    'property-no-vendor-prefix': null,
    'selector-not-notation': null,
    'no-descending-specificity': null,
    'media-feature-range-notation': null,
    'number-max-precision': null,
    'custom-property-empty-line-before': null,
    'value-keyword-case': null,
    'font-family-name-quotes': null,
    'color-hex-length': null,
    'rule-empty-line-before': null,
    'comment-empty-line-before': null,
    'shorthand-property-no-redundant-values': null,
    'at-rule-no-unknown': [
      true,
      {
        ignoreAtRules: ['tailwind', 'apply', 'variants', 'responsive', 'screen', 'layer'],
      },
    ],
  },
};
