/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{vue,js,ts,jsx,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        primary: {
          50: '#eff8fc',
          100: '#d7eefa',
          200: '#b3e2f6',
          300: '#7ed0ef',
          400: '#41b6e2',
          500: '#1a9dce',
          600: '#0f7fae',
          700: '#10698f',
          800: '#145976',
          900: '#154a62',
          950: '#0b2d3f'
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
          50: '#f8fafc',
          100: '#f1f5f9',
          200: '#e2e8f0',
          300: '#cbd5e1',
          400: '#94a3b8',
          500: '#64748b',
          600: '#475569',
          700: '#334155',
          800: '#242a34',
          900: '#171b22',
          950: '#0c0e12'
        },
        surface: {
          50: '#ffffff',
          100: '#fafbfc',
          200: '#f3f5f7',
          300: '#e9edf1',
          400: '#dce2e8',
          500: '#c8d0d9',
          600: '#aeb8c3',
          700: '#7f8a96',
          800: '#4f5a66',
          900: '#2b333d',
          950: '#10151b'
        },
        ink: {
          50: '#f6f8f9',
          100: '#e9eef1',
          200: '#d4dce1',
          300: '#aab8c1',
          400: '#7e8f9a',
          500: '#5a6a75',
          600: '#44515b',
          700: '#38434c',
          800: '#242c33',
          900: '#181e24',
          950: '#0d1115'
        },
        borderline: {
          100: '#edf0f3',
          200: '#e2e7eb',
          300: '#d3d9df',
          400: '#bfc8d0',
          500: '#9da9b4',
          600: '#7f8c98'
        },
        // 旧 gray-* 用法全局收敛到 ink 中性阶，避免 2000+ 处散落色值不一致
        gray: {
          50: '#f6f8f9',
          100: '#e9eef1',
          200: '#d4dce1',
          300: '#aab8c1',
          400: '#7e8f9a',
          500: '#5a6a75',
          600: '#44515b',
          700: '#38434c',
          800: '#242c33',
          900: '#181e24',
          950: '#0d1115'
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
        // 统一收敛到克制的层级：不再有发光/玻璃特效
        sm: '0 1px 2px rgba(16, 24, 40, 0.05)',
        DEFAULT: '0 1px 2px rgba(16, 24, 40, 0.05)',
        md: '0 2px 6px rgba(16, 24, 40, 0.07)',
        lg: '0 4px 12px rgba(16, 24, 40, 0.08)',
        xl: '0 8px 20px rgba(16, 24, 40, 0.09)',
        '2xl': '0 12px 28px rgba(16, 24, 40, 0.10)',
        glass: '0 1px 2px rgba(16, 24, 40, 0.05)',
        'glass-sm': '0 1px 2px rgba(16, 24, 40, 0.04)',
        glow: '0 0 0 rgba(20, 184, 166, 0)',
        'glow-lg': '0 0 0 rgba(20, 184, 166, 0)',
        card: '0 1px 2px rgba(16, 24, 40, 0.05)',
        'card-hover': '0 4px 12px rgba(16, 24, 40, 0.08)',
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
        // 旧类名统一收敛：任何 rounded-xl/2xl/3xl 都落在 8px 体系内
        sm: '4px',
        DEFAULT: '6px',
        md: '6px',
        lg: '8px',
        xl: '8px',
        '2xl': '8px',
        '3xl': '10px',
        control: '6px',
        panel: '8px',
        dialog: '12px'
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
