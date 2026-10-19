/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{vue,js,ts,jsx,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        primary: {
          // Microsoft Fluent 2 official blue scale (500 = #0078d4)
          50: '#ecf7ff',
          100: '#d0ebff',
          200: '#9dd8ff',
          300: '#54b9f9',
          400: '#2ea6f2',
          500: '#0078d4',
          600: '#0061a4',
          700: '#00508d',
          800: '#003c71',
          900: '#002e56',
          950: '#001e38'
        },
        accent: {
          50: '#f8fafc',
          100: '#f1f5f9',
          200: '#e2e8f0',
          300: '#cbd5e1',
          400: '#94a3b8',
          500: '#64748b',
          600: '#475569',
          700: '#334155',
          800: '#1e293b',
          900: '#0f172a',
          950: '#020617'
        },
        dark: {
          // Fluent dark neutrals (cool)
          50: '#f8f9fa',
          100: '#eef0f2',
          200: '#e0e3e7',
          300: '#c6cad1',
          400: '#9aa0a9',
          500: '#6d7480',
          600: '#5a6068',
          700: '#3b4048',
          800: '#27292d',
          900: '#1b1a19',
          950: '#0f0e0d'
        },
        surface: {
          50: '#ffffff',
          100: '#faf9f8',
          200: '#f4f3f2',
          300: '#f0efef',
          400: '#eae6e5',
          500: '#e0dfdf',
          600: '#cdcdcd',
          700: '#b3b3b3',
          800: '#605e5c',
          900: '#3b3a39',
          950: '#1b1a19'
        },
        ink: {
          // Cool neutral scale (slightly blue-tinted)
          50: '#fafafa',
          100: '#f3f4f6',
          200: '#e5e7eb',
          300: '#d2d6dc',
          400: '#a3a9b3',
          500: '#6d7480',
          600: '#5c626d',
          700: '#44484f',
          800: '#2e3036',
          900: '#1f2126',
          950: '#101215'
        },
        borderline: {
          100: '#f0efef',
          200: '#e6e5e4',
          300: '#d2d0ce',
          400: '#b3b0ae',
          500: '#8a8886',
          600: '#605e5c'
        },
        // 旧 gray-* 用法全局收敛到 ink 中性阶，避免 2000+ 处散落色值不一致
        gray: {
          50: '#fafafa',
          100: '#f3f4f6',
          200: '#e5e7eb',
          300: '#d2d6dc',
          400: '#a3a9b3',
          500: '#6d7480',
          600: '#5c626d',
          700: '#44484f',
          800: '#2e3036',
          900: '#1f2126',
          950: '#101215'
        }
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
          glow: '0 0 0 rgba(0, 120, 212, 0)',
          'glow-lg': '0 0 0 rgba(0, 120, 212, 0)',
          'inner-glow': 'inset 0 1px 0 rgba(255, 255, 255, 0.5)'
        },
      backgroundImage: {
        'gradient-radial': 'radial-gradient(var(--tw-gradient-stops))',
        'gradient-primary': 'linear-gradient(180deg, #065f9d 0%, #0b7baa 100%)',
        'gradient-dark': 'linear-gradient(180deg, #242a34 0%, #171b22 100%)',
        'gradient-glass': 'linear-gradient(180deg, rgba(255,255,255,0.85) 0%, rgba(255,255,255,0.95) 100%)',
        'mesh-gradient':
          'radial-gradient(at 40% 20%, rgba(26, 157, 206, 0.05) 0px, transparent 50%), radial-gradient(at 80% 0%, rgba(26, 157, 206, 0.03) 0px, transparent 50%), radial-gradient(at 0% 50%, rgba(26, 157, 206, 0.02) 0px, transparent 50%)'
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
