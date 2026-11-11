"""Browser regression for shared theme colors and the mobile dock.

Run against a local frontend server; all API calls use test fixtures.
Requires Python Playwright and its Chromium browser (no real credentials).
"""
import argparse
import json
import sys
from pathlib import Path
from urllib.parse import urlparse
from playwright.sync_api import sync_playwright

COLOR = """el => {
  const parse = value => { const n=value.match(/[0-9.]+/g).map(Number); return [n[0], n[1], n[2], n.length>3?n[3]:1]; };
  const over = (top, bottom) => [0,1,2].map(i=>top[i]*top[3]+bottom[i]*(1-top[3]));
  const bg = node => { if(!node) return [255,255,255]; const c=parse(getComputedStyle(node).backgroundColor); return over(c, bg(node.parentElement)); };
  const linear = c => (c/=255)<=0.04045 ? c/12.92 : ((c+0.055)/1.055)**2.4;
  const lum = c => c.map(linear).reduce((s,v,i)=>s+v*[0.2126,0.7152,0.0722][i],0);
  const background=bg(el), foreground=over(parse(getComputedStyle(el).color),background);
  const a=lum(foreground), b=lum(background);
  return {foreground, background, ratio:(Math.max(a,b)+0.05)/(Math.min(a,b)+0.05)};
}"""

USER={'id':99991,'email':'review@example.test','username':'review','role':'admin','status':'active','balance':100,'concurrency':5,'subscriptions':[]}
ACCOUNT={'id':991,'name':'Review fixture account','platform':'openai','type':'apikey','status':'active','schedulable':True,'priority':1,'concurrency':5,'group_ids':[],'groups':[],'credentials':{'base_url':'https://example.test','api_key':'fixture-only'},'extra':{},'created_at':'2026-11-01T00:00:00Z','updated_at':'2026-11-01T00:00:00Z'}

def mock(route):
    path=urlparse(route.request.url).path
    data={}
    if path.endswith('/setup/status'): data={'needs_setup':False,'step':'completed'}
    elif path.endswith('/auth/me') or path.endswith('/user/profile'): data=USER
    elif path.endswith('/settings/public'): data={'site_name':'Review test','registration_enabled':True,'version':'review-fixture','run_mode':'standard'}
    elif path.endswith('/admin/settings/admin-api-key'): data={'exists':True,'masked_key':'review-fixture-masked'}
    elif path.endswith('/admin/accounts'): data={'items':[ACCOUNT],'total':1,'page':1,'page_size':20,'pages':1}
    elif path.endswith('/admin/accounts/991'): data=ACCOUNT
    elif '/proxies' in path or '/groups/all' in path or path.endswith('/providers'): data=[]
    elif '/groups' in path: data={'items':[],'total':0,'page':1,'page_size':20}
    elif '/compliance' in path: data={'required':False}
    route.fulfill(json={'code':0,'data':data})

def run(args):
    results=[]
    errors=[]
    def check(name, passed, **evidence):
        results.append({'name':name,'passed':bool(passed),**evidence})
        print(json.dumps(results[-1], ensure_ascii=False), flush=True)
    with sync_playwright() as p:
        browser=p.chromium.launch(headless=True)
        try:
            context=browser.new_context(viewport={'width':1280,'height':900})
            context.add_init_script("""localStorage.setItem('auth_token','review-fixture'); localStorage.setItem('auth_user',JSON.stringify(%s)); localStorage.setItem('theme','dark'); localStorage.setItem('admin_guide_99991_admin_v4_interactive','true');""" % json.dumps(USER))
            context.route('**/api/v1/**',mock)
            context.route('**/setup/status',mock)
            page=context.new_page()
            page.on('pageerror',lambda error:errors.append(str(error)))
            page.goto(args.base_url+'/admin/settings',wait_until='networkidle')
            page.get_by_role('tab',name='安全与认证',exact=True).click()
            key=page.get_by_text('review-fixture-masked',exact=True)
            key.wait_for(state='visible')
            key.scroll_into_view_if_needed()
            color=key.evaluate(COLOR)
            check('dark settings populated API key',color['ratio']>=4.5,**color)
            if args.screenshots:
                Path(args.screenshots).mkdir(parents=True,exist_ok=True)
                page.screenshot(path=str(Path(args.screenshots)/'settings-dark.png'))

            page.goto(args.base_url+'/admin/accounts',wait_until='networkidle')
            page.get_by_role('button',name='编辑',exact=True).click()
            option=page.locator('[data-testid="openai-endpoint-capability-chat_completions"]').locator('..').locator('span')
            option.wait_for(state='visible')
            option.scroll_into_view_if_needed()
            color=option.evaluate(COLOR)
            check('dark populated account endpoint option',color['ratio']>=4.5,**color)
            if args.screenshots:
                page.screenshot(path=str(Path(args.screenshots)/'account-dark.png'))

            page.goto(args.base_url+'/admin/settings',wait_until='networkidle')
            for width in (375,640,768,1023,1024):
                page.set_viewport_size({'width':width,'height':800})
                page.evaluate('window.scrollTo(0,document.documentElement.scrollHeight)')
                page.wait_for_timeout(250)
                save=page.get_by_role('button',name='保存设置',exact=True)
                save.wait_for(state='visible')
                button=save.bounding_box()
                dock=page.locator('.mobile-dock')
                dock_box=dock.bounding_box() if dock.is_visible() else None
                padding=page.locator('main').evaluate('el=>getComputedStyle(el).paddingBottom')
                uncovered=not dock_box or button['y']+button['height']<=dock_box['y']
                check('bottom action at '+str(width),uncovered,padding=padding,button_bottom=button['y']+button['height'],dock_top=dock_box['y'] if dock_box else None)
                if uncovered:
                    save.click(trial=True)

            page.set_viewport_size({'width':1280,'height':900})
            page.evaluate("""() => {
              const section=document.createElement('section'); section.id='theme-button-probes';
              section.style.cssText='position:fixed;left:20px;top:80px;z-index:2147483640;padding:16px;background:rgb(var(--surface))';
              for(const kind of ['danger','success','warning']) {
                const b=document.createElement('button'); b.id='probe-'+kind; b.type='button'; b.className='btn btn-'+kind; b.textContent=kind; section.appendChild(b);
              } document.body.appendChild(section);
            }""")
            for theme in ('light','dark'):
                page.evaluate("theme=>document.documentElement.classList.toggle('dark',theme==='dark')",theme)
                for kind in ('danger','success','warning'):
                    button=page.locator('#probe-'+kind)
                    for state in ('default','hover','active'):
                        page.mouse.move(1270,10)
                        if state!='default': button.hover()
                        if state=='active': page.mouse.down()
                        page.wait_for_timeout(250)
                        color=button.evaluate(COLOR)
                        check(theme+' '+kind+' '+state,color['ratio']>=4.5,**color)
                        if state=='active': page.mouse.up()
            check('no uncaught page errors',not errors,errors=errors)
            context.close()
        finally:
            browser.close()
    report={'passed':sum(r['passed'] for r in results),'total':len(results),'checks':results}
    if args.report:
        Path(args.report).parent.mkdir(parents=True,exist_ok=True)
        Path(args.report).write_text(json.dumps(report,ensure_ascii=False,indent=2),encoding='utf-8')
    print(json.dumps(report,ensure_ascii=False,indent=2))
    return 0 if report['passed']==report['total'] else 1

if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--base-url',required=True)
    parser.add_argument('--report')
    parser.add_argument('--screenshots')
    sys.exit(run(parser.parse_args()))
