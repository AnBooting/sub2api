/** @type {import('tailwindcss').Config} */
// 令牌来源：frontend/src/styles/design-system.css
// 约定：这里只做「CSS 变量 → Tailwind 名称」映射，不写死色值。
const withVar = (name) => `rgb(var(--c-${name}) / <alpha-value>)`
const semantic = (name) => `rgb(var(--${name}) / <alpha-value>)`

function palette(family) {
  const steps = [50, 100, 200, 300, 400, 500, 600, 700, 800, 900, 950]
  return Object.fromEntries(steps.map((step) => [step, withVar(`${family}-${step}`)]))
}

// Foreground status shades adapt to the theme; solid controls keep dark fills
// so their light labels stay readable in either theme and every pointer state.
function statusPalette(family) {
  return {
    ...palette(family),
    solid: {
      DEFAULT: withVar(`${family}-solid`),
      hover: withVar(`${family}-solid-hover`),
      active: withVar(`${family}-solid-active`)
    }
  }
}

/**
 * 封闭调色板：覆盖 theme.colors（不是 extend），Tailwind 默认 22 个色族不注册。
 * 写了 bg-emerald-500 之类不会生效 —— 审查会抓出来，逼着用语义令牌。
 */
const COLORS = {
  transparent: 'transparent',
  current: 'currentColor',
  inherit: 'inherit',

  // 纸与墨：白 = 纸面 surface，黑 = 主墨 ink
  white: semantic('surface'),
  black: semantic('ink'),

  // 纸/墨色阶（暖石灰 / 暖深褐）
  gray: palette('gray'),
  dark: palette('graphite'),

  // 信号：克莱因钴蓝。别名 primary 让既有 class 自动换色
  primary: palette('cobalt'),
  cobalt: palette('cobalt'),

  // 状态
  success: statusPalette('moss'),
  warning: statusPalette('ochre'),
  danger: statusPalette('oxide'),

  // 分类（归属/类型）：默认 azure，另有四个同明度的分类色相供区分平台
  accent: palette('azure'),
  'tax-blue': palette('tax-blue'),
  'tax-plum': palette('tax-plum'),
  'tax-sage': palette('tax-sage'),
  'tax-clay': palette('tax-clay'),
  // 图表第 6 序列专用
  lilac: palette('lilac'),

  // 语义名称
  paper: { DEFAULT: semantic('paper'), inset: semantic('paper-inset') },
  surface: { DEFAULT: semantic('surface'), raised: semantic('surface-raised') },
  ink: {
    DEFAULT: semantic('ink'),
    soft: semantic('ink-soft'),
    faint: semantic('ink-faint')
  },
  rule: { DEFAULT: semantic('rule'), strong: semantic('rule-strong') },
  signal: semantic('signal'),
  // 压在强色（signal/danger/success/warning）上的字：永远亮，不随纸面翻色
  'on-signal': semantic('on-signal')
}

export default {
  content: ['./index.html', './src/**/*.{vue,js,ts,jsx,tsx}'],
  darkMode: 'class',
  theme: {
    colors: COLORS,
    extend: {
      fontFamily: {
        // 界面与读数
        sans: ['var(--font-ui)'],
        read: ['var(--font-read)'],
        mono: ['var(--font-read)'],
        // 展示：编辑体衬线。标题、大数字、品牌名用这一族。
        display: ['var(--font-display)']
      },
      fontStretch: { 'semi-condensed': '88%', normal: '100%', wide: '103%' },
      fontSize: {
        // 编辑体字号阶：衬线在小字号会糊，所以标题从 1.25rem 起跳
        'display-sm': ['1.375rem', { lineHeight: '1.25', letterSpacing: 'var(--track-display)' }],
        'display-md': ['1.875rem', { lineHeight: '1.15', letterSpacing: 'var(--track-display)' }],
        'display-lg': ['2.5rem', { lineHeight: '1.08', letterSpacing: 'var(--track-display)' }],
        'display-xl': ['3.5rem', { lineHeight: '1.02', letterSpacing: 'var(--track-display)' }]
      },
      borderRadius: {
        // 切角取代圆角：3px 基准，最大 6px；full 仅头像/开关/圆点
        sm: '2px',
        DEFAULT: '3px',
        md: '4px',
        lg: '5px',
        xl: '6px',
        '2xl': '6px',
        '3xl': '6px',
        '4xl': '6px',
        full: '999px'
      },
      boxShadow: {
        // 唯一允许的柔影：真正浮起来的层。暖色投影，不用纯黑。
        float: '0 18px 44px -26px rgb(58 53 48 / 0.42)',
        'float-sm': '0 10px 26px -20px rgb(58 53 48 / 0.36)',
        none: 'none'
      },
      transitionTimingFunction: {
        cut: 'cubic-bezier(0.22, 0.61, 0.24, 1)',
        settle: 'cubic-bezier(0.16, 0.84, 0.24, 1)'
      },
      transitionDuration: { fast: '140ms', base: '240ms' },
      spacing: { touch: '44px', dock: '4.75rem', gutter: 'var(--gutter)' },
      maxWidth: { measure: '68ch' },
      keyframes: {
        // 唯一编排瞬间：痕量线被画出
        'trace-draw': { from: { transform: 'scaleX(0)' }, to: { transform: 'scaleX(1)' } },
        // 衬线标题的一次性入场：只做微小的字距收束，不做位移
        'type-settle': {
          from: { opacity: '0', letterSpacing: '0.04em' },
          to: { opacity: '1', letterSpacing: 'var(--track-display)' }
        },
        'ink-in': {
          from: { opacity: '0', transform: 'translateY(4px)' },
          to: { opacity: '1', transform: 'translateY(0)' }
        }
      },
      animation: {
        'trace-draw': 'trace-draw var(--motion-trace) var(--ease-settle) both',
        'type-settle': 'type-settle var(--motion-trace) var(--ease-settle) both',
        'ink-in': 'ink-in var(--motion-base) var(--ease-cut) both'
      }
    }
  },
  plugins: []
}
