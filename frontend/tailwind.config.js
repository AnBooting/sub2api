/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{vue,js,ts,jsx,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        primary: {
          // 暖陶土主色（低饱和自然材质）。亮度校准过：600 起白字达 WCAG AA(4.98)，700 达 AAA(7.0)
          50: '#fdf9f4',
          100: '#faeddc',
          200: '#f3d5b3',
          300: '#e9b884',
          400: '#dd9757',
          500: '#c9762f',
          600: '#a85c22',
          700: '#87491d',
          800: '#6e3c1a',
          900: '#5a3218',
          950: '#31190b'
        },
        accent: {
          // 暖中性辅助阶
          50: '#faf9f7',
          100: '#f2f0ec',
          200: '#e4e0d9',
          300: '#cdc7bc',
          400: '#a8a094',
          500: '#7a7267',
          600: '#5f584e',
          700: '#48423a',
          800: '#302b25',
          900: '#201d18',
          950: '#12100c'
        },
        dark: {
          // 暖炭灰深色面（保留原亮度层级，仅把冷蓝调改成暖褐调）
          50: '#f9f8f5',
          100: '#f0eeea',
          200: '#e2ded6',
          300: '#c9c3b7',
          400: '#9e968a',
          500: '#736c60',
          600: '#59534a',
          700: '#3c3831',
          800: '#292520',
          900: '#1a1713',
          950: '#0f0d0a'
        },
        surface: {
          // 暖纸白表面
          50: '#ffffff',
          100: '#fdfbf8',
          200: '#f8f4ee',
          300: '#f3eee6',
          400: '#ece5da',
          500: '#e1d9cc',
          600: '#cec4b4',
          700: '#b3a795',
          800: '#635a4c',
          900: '#3a352c',
          950: '#1b1813'
        },
        ink: {
          // 暖中性文字阶
          50: '#faf9f7',
          100: '#f4f2ed',
          200: '#e7e3dc',
          300: '#d4cec4',
          400: '#a8a094',
          500: '#766e62',
          600: '#5f584e',
          700: '#47413a',
          800: '#302b25',
          900: '#211d18',
          950: '#12100c'
        },
        borderline: {
          // 暖色描边
          100: '#f2efea',
          200: '#e8e4dd',
          300: '#d5cfc6',
          400: '#b5ada1',
          500: '#8c8478',
          600: '#635c51'
        },

        // 旧 gray-* 用法全局收敛到 ink 中性阶，避免 2000+ 处散落色值不一致
        gray: {
          // 与 ink 同阶（保持既有 gray-* 引用不失效）
          50: '#faf9f7',
          100: '#f4f2ed',
          200: '#e7e3dc',
          300: '#d4cec4',
          400: '#a8a094',
          500: '#766e62',
          600: '#5f584e',
          700: '#47413a',
          800: '#302b25',
          900: '#211d18',
          950: '#12100c'
        },
      },
      fontFamily: {
        sans: [
          'Segoe UI',
          'system-ui',
          '-apple-system',
          'BlinkMacSystemFont',
          'Roboto',
          'Helvetica Neue',
          'Arial',
          'PingFang SC',
          'Hiragino Sans GB',
          'Microsoft YaHei',
          'sans-serif'
        ],
        mono: ['ui-monospace', 'SFMono-Regular', 'Menlo', 'Monaco', 'Consolas', 'monospace']
      },
      boxShadow: {
          // Fluent 2 depth：阴影偏冷、扩散大、几乎不可见，但提供层叠暗示
          none: 'none',
          sm: '0 1px 2px rgba(0, 0, 0, 0.05)',
          DEFAULT: '0 1px 2px rgba(0, 0, 0, 0.05)',
          md: '0 2px 4px rgba(0, 0, 0, 0.06)',
          lg: '0 4px 8px rgba(0, 0, 0, 0.07)',
          xl: '0 8px 16px rgba(0, 0, 0, 0.08)',
          '2xl': '0 12px 28px rgba(0, 0, 0, 0.10)',
          '4xl': '0 24px 56px rgba(0, 0, 0, 0.14)',
          // Fluent 层叠深度：卡片 / 悬浮 / 浮层
          card: '0 1px 2px rgba(0, 0, 0, 0.04), 0 0 1px rgba(0, 0, 0, 0.03)',
          'card-hover': '0 4px 10px rgba(0, 0, 0, 0.07), 0 0 1px rgba(0, 0, 0, 0.04)',
          // 亚克力材质：底色半透 + 冷色内描边 + 柔和投影
          acrylic: '0 2px 8px rgba(0, 0, 0, 0.06), 0 0 1px rgba(255, 255, 255, 0.6) inset',
          'acrylic-elevated': '0 8px 24px rgba(0, 0, 0, 0.10), 0 0 1px rgba(255, 255, 255, 0.7) inset',
          glass: '0 1px 2px rgba(0, 0, 0, 0.05)',
          'glass-sm': '0 1px 2px rgba(0, 0, 0, 0.04)',
          glow: '0 0 0 rgba(201, 118, 47, 0)',
          'glow-lg': '0 0 0 rgba(201, 118, 47, 0)',
          'inner-glow': 'inset 0 1px 0 rgba(255, 255, 255, 0.5)'
        },
      backgroundImage: {
        'gradient-radial': 'radial-gradient(var(--tw-gradient-stops))',
        'gradient-primary': 'linear-gradient(180deg, #a85c22 0%, #87491d 100%)',
        'gradient-dark': 'linear-gradient(180deg, #2a2520 0%, #17130f 100%)',
        'gradient-glass': 'linear-gradient(180deg, rgba(255,255,255,0.85) 0%, rgba(255,255,255,0.95) 100%)',
        'mesh-gradient':
          'radial-gradient(at 40% 20%, rgba(201, 118, 47, 0.05) 0px, transparent 50%), radial-gradient(at 80% 0%, rgba(201, 118, 47, 0.03) 0px, transparent 50%), radial-gradient(at 0% 50%, rgba(201, 118, 47, 0.02) 0px, transparent 50%)'
      },
      borderRadius: {
          // Fluent 2: 略方圆角，控件 4px / 卡片 8px / 弹窗 10px
          none: '0px',
          sm: '2px',
          DEFAULT: '4px',
          md: '4px',
          lg: '6px',
          xl: '8px',
          '2xl': '8px',
          '3xl': '10px',
          control: '4px',
          panel: '8px',
          dialog: '10px',
          pill: '9999px'
        },
      animation: {
        'fade-in': 'fadeIn 0.18s ease-out',
        'slide-up': 'slideUp 0.18s ease-out',
        'slide-down': 'slideDown 0.18s ease-out',
        'slide-in-right': 'slideInRight 0.2s ease-out',
        'scale-in': 'scaleIn 0.14s ease-out',
        'pulse-slow': 'pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite',
        shimmer: 'shimmer 1.8s linear infinite',
        glow: 'glow 2s ease-in-out infinite alternate'
      },
      keyframes: {
        fadeIn: {
          '0%': { opacity: '0' },
          '100%': { opacity: '1' }
        },
        slideUp: {
          '0%': { opacity: '0', transform: 'translateY(6px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' }
        },
        slideDown: {
          '0%': { opacity: '0', transform: 'translateY(-6px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' }
        },
        slideInRight: {
          '0%': { opacity: '0', transform: 'translateX(12px)' },
          '100%': { opacity: '1', transform: 'translateX(0)' }
        },
        scaleIn: {
          '0%': { opacity: '0', transform: 'scale(0.98)' },
          '100%': { opacity: '1', transform: 'scale(1)' }
        },
        shimmer: {
          '0%': { backgroundPosition: '-200% 0' },
          '100%': { backgroundPosition: '200% 0' }
        },
        glow: {
          '0%': { opacity: '0.6' },
          '100%': { opacity: '1' }
        }
      }
    }
  },
  plugins: []
}
