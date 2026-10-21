import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { createRouter, createMemoryHistory, RouterView } from 'vue-router'
import { readFileSync } from 'node:fs'
import { expect, it, vi } from 'vitest'
import LoginView from '@/views/auth/LoginView.vue'

vi.mock('vue-i18n', () => ({
 createI18n: () => ({global:{t:(key:string)=>key}}),
 useI18n: () => ({t:(key:string)=>key})
}))
vi.mock('@/stores',()=>({
 useAuthStore:()=>({login:vi.fn(),loginWithPasskey:vi.fn(),login2FA:vi.fn()}),
 useAppStore:()=>({showError:vi.fn(),showSuccess:vi.fn(),showWarning:vi.fn()})
}))
vi.mock('@/api/auth',()=>({
 buildOAuthLoginStartURL:vi.fn(),getPublicSettings:vi.fn().mockResolvedValue({registration_enabled:true}),
 isTotp2FARequired:vi.fn(()=>false),isWeChatWebOAuthEnabled:vi.fn(()=>false),startOAuthLogin:vi.fn()
}))

it('production App route outlet can leave the actual LoginView', async()=>{
 const source=readFileSync('src/App.vue','utf8')
 const outlet=source.match(/<RouterView v-slot[\s\S]*?<\/RouterView>/)?.[0] || '<router-view />'
 const router=createRouter({history:createMemoryHistory(),routes:[
  {path:'/login',component:LoginView},
  {path:'/next',component:{template:'<div data-testid="next-route">Next page</div>'}}
 ]})
 await router.push('/login'); await router.isReady()
 const wrapper=mount(defineComponent({components:{RouterView},template:outlet}),{
  attachTo:document.body,
  global:{plugins:[router],stubs:{
   AuthLayout:{template:'<div><slot /><slot name="footer" /></div>'},
   DingTalkOAuthSection:true,EmailOAuthButtons:true,Icon:true,LinuxDoOAuthSection:true,
   LoginAgreementPrompt:true,OidcOAuthSection:true,TotpLoginModal:true,TurnstileWidget:true,WechatOAuthSection:true,
   transition:false
  }}
 })
 try{
  await flushPromises()
  expect(wrapper.text()).toContain('auth.signUp')
  await router.push('/next');await flushPromises()
  await new Promise(resolve=>setTimeout(resolve,100));await flushPromises()
  expect(wrapper.find('[data-testid="next-route"]').exists()).toBe(true)
 }finally{wrapper.unmount()}
})
