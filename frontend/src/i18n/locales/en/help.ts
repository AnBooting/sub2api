/**
 * In-app help page copy (user guide + admin guide).
 *
 * Source: project-docs/用户使用手册.md and project-docs/管理员使用手册.md (2026-10-21).
 * Rule: describe only what is live today; entries that are not enabled yet must say so
 * explicitly instead of promising functionality.
 *
 * Structure contract (rendered by HelpView.vue):
 *   help.<section>.title      required section heading
 *   help.<section>.intro      required lead paragraph
 *   help.<section>.bullets    optional Record<slug, string> rendered as a bullet list
 *   help.<section>.rows       optional Record<slug, { t, d }> rendered as a two-column table
 *   help.<section>.tableHead  optional { t, d } table header for rows
 *   help.<section>.items      optional Record<slug, { q, a }> rendered as a Q/A list
 *   help.<section>.note       optional closing note
 *
 * Keep message-syntax characters (braces, pipes, at-signs) out of this file.
 */
export default {
  help: {
    title: 'Help Center',
    subtitle:
      'The billing and scheduling rules that are live on this site: where the money goes, why this call cost more than the last one, and how to investigate a failure.',
    userTitle: 'User Guide',
    adminTitle: 'Admin Guide',
    adminIntro: 'The sections below are for site owners and operators; regular users can skip them.',

    user: {
      credit: {
        title: 'What Credit is and how billing is computed',
        intro:
          'Balance, spend, key quota and group limits are all priced in Credit. The top-up ratio is fixed at ¥1 = 1 Credit. CNY source prices for domestic models and USD source prices for foreign models are mapped one-to-one to a base Credit price, with no currency conversion.',
        bullets: {
          formula:
            'Billing formula: Credit consumed = effective multiplier of the group that actually served the request × Σ(base Credit price × normalized usage).',
          basePrice:
            'Base price: the base Credit price of the model in the price table, quoted per 1M tokens or per call depending on the metric.',
          multiplier:
            'Multiplier: the group multiplier (which may include peak/off-peak and video-specific multipliers). It comes from the group that actually served the request, not the group you picked on the dashboard.',
          sum:
            'Summed per component: input, output, cached and media-duration items are priced separately and then added up; cached tokens are not billed twice.',
          lock:
            'Price lock snapshot: at the moment the request starts, the start time, price version, base price, actual serving group and the multiplier at that moment are locked. Changing prices or multipliers while a request is running does not affect it, and internal retries do not re-price.',
          missing:
            'Missing price is not free: when a model has no valid price, the platform rejects the request before it reaches the upstream (error pricing_unavailable). It is never let through at zero cost and never falls back to a GPT default price; a model explicitly configured at 0 is billed at 0 as a normal price.'
        },
        note: 'Official source price amounts are not shown to users. Usage records only contain the base price converted to Credit, plus currency and source metadata.'
      },

      peak: {
        title: 'Peak and off-peak hours: detected automatically from official pricing',
        intro:
          'Official DeepSeek pricing comes with peak and off-peak hours. The platform detects which one applies using the official rules, so you never switch it yourself.',
        bullets: {
          window:
            'Detection is fixed to the Asia/Shanghai time zone. On weekdays (excluding public holidays) the peak windows are 09:00-12:00 and 14:00-18:00; every other time is off-peak.',
          holiday:
            'The public-holiday calendar takes priority: maintained holidays are billed at the off-peak rate, and weekends are off-peak too, including weekends that are make-up workdays.',
          multiplier: 'Peak hours are billed at 2x, off-peak hours at 1x; the multiplier that applies is the one for the current period.',
          display:
            'The group badge shows whether the group is currently at peak or off-peak and which multiplier applies. The badge only reflects the current state of the group, it does not take part in billing, and the billing rules still follow the backend price table.',
          custom:
            'If the group has its own peak window configured (the site owner sets it in the group form), that configured window and multiplier win; the official DeepSeek window is only the fallback.',
          lock:
            'Peak and off-peak detection follows price lock like everything else: the state at the moment the request starts decides the pricing of that request, and crossing into another period does not change it.',
          scope:
            'Scope: the peak/off-peak rules only apply to models and groups that use the official DeepSeek price card. Groups on other platforms are not covered and are shown as not applicable.'
        },
        note:
          'If a year has neither a maintained holiday calendar nor built-in coverage, the platform disables the official peak surcharge for that year (everything is billed off-peak) instead of charging the weekday peak rate.'
      },

      groups: {
        title: 'Regular group order and high-availability fallback',
        intro:
          'Requests are scheduled by the regular group order configured on your key. The high-availability fallback is an extra stability option: it is off by default and you have to turn it on.',
        bullets: {
          order:
            'Order is priority: on the create or edit key page you can move groups up, move them down, or remove them. When nothing is bound, the list shows "no regular group bound". The public standard group is always available; dedicated groups still require authorization or a subscription. Old single-group keys are treated as a one-item order list and behave exactly as before.',
          failover:
            'When the platform switches groups: it first looks for a usable account inside the current group, and only tries the next group once that group runs out of candidates (account invalid, rate limited, at capacity, or a site-side failure).',
          noSwap:
            'When nothing is retried: user-side errors such as an invalid key, insufficient balance, bad parameters, an invalid model name, or a content-moderation rejection are returned directly without switching account or group.',
          fallbackOff:
            'Fallback is off by default: you have to click "enable fallback". The entry only appears when editing an existing key (it is not in the create-key form), so create the key first and come back.',
          fallbackScope:
            'When fallback is available: you only see the "enable fallback" button if the operator configured usable fallback groups. A dedicated group needs access authorization first, and a group whose model range does not match the key cannot be enabled.',
          fallbackBilling:
            'Fallback billing: once enabled, billing still uses the actual serving group (a fallback group may carry a higher multiplier), and usage records show the failover marker.',
          rateLimit:
            'Group rate limits: each group can define a request rate limit (QPS) and a per-user concurrency limit. Exceeding them makes requests rejected or queued, which is normal rate-limit behaviour, not a fault.'
        }
      },

      pricingTable: {
        title: 'Missing prices and the price overview: how to check pricing',
        intro:
          'A pricing_unavailable error is a site-side pricing configuration problem, not a problem with your key or your balance. The site owner can check it directly in the missing-price list and the price overview.',
        bullets: {
          meaning:
            'What a missing price means: when a model has no valid price configured, the platform rejects the request before it reaches the upstream (pricing_unavailable) and never lets it through at zero cost. There is nothing for you to do; send the site owner the model name and the timestamp.',
          adminTable:
            'Price overview (the "Price overview" view on the admin channel pricing page): a read-only summary of the models that appear in channel and group pricing rules, with their currency, pricing unit and each unit price.',
          adminMissing:
            'Missing-price list (the "Missing price list" view on the same page): lists models that are configured but have incomplete prices, states whether the input price, output price, cache price or image price is missing, and links to the pricing editor.',
          probe:
            'Detection basis: the "default price card" column in both views is produced by probing the current default price card model by model, so it may differ from actual billing. Wildcard rules are not probed per model.',
          scope:
            'Coverage: the default price card only offers a single-model query, so the price overview lists only the models that appear in channel and group rules; it is not a full catalogue of every model on the site.'
        },
        note:
          'Those two admin views are for the site owner and operators. What you see in usage records is the actual charge converted into Credit plus the pricing snapshot; official source price amounts are not shown.'
      },

      recharge: {
        title: 'Top-up and redemption: two paths, different states',
        intro: 'There are currently two top-up paths with different states. Check the entry before you operate.',
        tableHead: { t: 'Entry', d: 'Current status and details' },
        rows: {
          purchase: {
            t: 'Recharge / Subscription (/purchase)',
            d: "The site's own payment flow, currently available; the balance arrives as soon as payment succeeds. Orders can be listed, cancelled, and refunded (depending on whether the channel supports refunds). After a network drop or refresh you can look the order up by order number or recovery credential."
          },
          shop: {
            t: 'Recharge Center (/shop)',
            d: 'A white-label product center (product, payment, card key, redemption). It is not enabled yet. When it is not configured, the page says "top-up service is not enabled / the site owner has not finished configuring the top-up service, please try again later or contact the site owner". Once enabled, a successful payment does not credit the balance automatically: you copy the card key and redeem it on the redemption page.'
          },
          redeem: {
            t: 'Redeem (/redeem)',
            d: 'Only site-issued redemption codes work. Codes are case-sensitive and can be redeemed once; they can add balance, add concurrency, or grant trial permissions. The failure message tells the reasons apart: this code has already been used / the code does not exist / the code has expired / the code is invalid.'
          }
        },
        bullets: {
          unit:
            'Two amount conventions: the in-site accounting unit is Credit, and balance, spend, key quota, subscription group limits, the redemption page, credited and bonus amounts on the top-up and order pages, the sidebar subscription progress card, the dashboard platform quota and batch image generation costs now all carry a "(Credit)" unit label. The Model Plaza price table shows source-currency prices (USD, no conversion) instead of account balance Credit; the real deduction is the Credit shown in usage records.',
          rate:
            'The recharge multiplier is a different convention: the "current rate" shown on the top-up and order pages describes the recharge rate, not the 1 CNY = 1 Credit ratio. The redemption page and the recharge center now state "¥1 gives 1 Credit (no conversion)" explicitly; the actual ratio follows the site configuration.'
        },
        note: 'Once the white-label product center is enabled, the recharge center has a "my orders" list where you can look up past orders and query them again; only the most recent order number is kept locally, so after a network drop or clearing browser data, give the site owner the order number shown in the notice.'
      },

      usage: {
        title: 'How to read usage records',
        intro: 'A record shows what was actually charged, who served the request, and how much was consumed.',
        tableHead: { t: 'Field', d: 'Meaning' },
        rows: {
          recordCost: { t: 'Recorded spend (Credit)', d: 'The Credit actually deducted for this request.' },
          snapshotCost: { t: 'Snapshot final spend', d: 'The final spend in the price-lock snapshot, kept for cross-checking.' },
          basePrice: { t: 'Base price (Credit) / stored subtotal (Credit)', d: 'The base price and subtotal of each billable component.' },
          actualGroup: { t: 'Actual serving group', d: 'The group that really served this request, which may not be the first one you ordered.' },
          failover: { t: 'Failover', d: '"No failover", or the failover reason and attempt count.' },
          usageDetail: {
            t: 'Usage detail',
            d: 'Input, output and cached tokens, or image count, video seconds, realtime-audio duration.'
          },
          pricingSnapshot: {
            t: 'Pricing snapshot',
            d: 'Source price (CNY or USD with its unit), base Credit price, group multiplier.'
          }
        },
        note: 'If "recorded spend" and "snapshot final spend" disagree, the page keeps both stored values for cross-checking and will not correct the ledger automatically. Do not resend the request; give the site owner the request_id and the timestamp of that record.'
      },

      media: {
        title: 'Billing status for images, video and realtime audio',
        intro: 'Media billing is described below as currently implemented; anything not covered yet is called out explicitly.',
        bullets: {
          image: 'Images: billed per image and per size, with independent multipliers on some models and batch image generation in the sidebar.',
          videoPerSecond:
            'Video (per second): cost = per-second price × duration, 1 to 15 seconds on the platform side, 8 seconds when unspecified, with 480p, 720p and 1080p tiers.',
          videoPerCall:
            'Video (per call): billed by request count. Per-second and per-call are mutually exclusive and configured per model × group; video may use an independent multiplier, separate from the text multiplier.',
          live: 'Realtime audio (Live / Realtime): billed by session duration, priced from the per-minute voice price configured on the group.',
          liveGap:
            'Current status: the Realtime per-minute price can now be configured for groups on every platform (the "Voice / Realtime explicit pricing" block of the admin group form). A group without a configured price settles that session as unbilled (the backend raises an alert). When Live consumption is 0, ask the site owner to confirm whether the group has a price configured.',
          visibility:
            'Visibility: the group badge shows the Live badge and the Realtime per-minute price (labelled as a source price, not the final charge); the usage record details show the Live session duration and the billing basis. Video tasks have their own "Video tasks" page (see that section), but the finished video is still not downloadable.',
          lock: 'Price lock: video and image tasks follow the same "locked at submission" rule.'
        }
      },

      videoTasks: {
        title: 'Video tasks: looking up your own video tasks',
        intro: 'The sidebar entry "Video tasks" shows the video generation tasks you submitted, their current state and their billing mode.',
        tableHead: { t: 'Field', d: 'Meaning' },
        rows: {
          taskNo: { t: 'Task number', d: 'The site-side task identifier; give this to the site owner when you need to cross-check.' },
          status: { t: 'Status', d: 'Processing / completed / failed.' },
          model: { t: 'Model and platform', d: 'The model this task actually used and the platform it ran on.' },
          billingMode: { t: 'Billing mode', d: 'Per token / per call / per second.' },
          createdAt: { t: 'Created at', d: 'When the task was created.' },
          settledAt: { t: 'Settled at', d: 'When the task finished settling; hidden while it is not settled yet.' },
          lastError: { t: 'Failure reason', d: 'Shown for failed tasks, empty for successful or in-progress ones.' }
        },
        bullets: {
          snapshot:
            'The billing mode comes from the billing snapshot taken when the task was created: the mode matched for your group and model at that moment is fixed, and later price changes do not change the billing mode of that task.',
          paging:
            'The list is ordered by creation time, newest first, 20 per page (up to 50). When more exist, click "Load more" to page through with the cursor.',
          scope: 'Only your own tasks are returned; internal fields such as the upstream task number are never included.'
        },
        note:
          'The video task page shows task and billing information only; it does not offer downloads of the finished video. Video charges are still authoritative in the usage records.'
      },

      faq: {
        title: 'Frequently asked questions',
        intro: 'The questions below come up most often, with the behaviour this site actually has.',
        items: {
          q1: {
            q: 'Why is this request more expensive than the last one?',
            a: 'Most often the actual serving group changed (a failover moved you to a fallback group with a higher multiplier). Compare "actual serving group" and "multiplier" in usage records to confirm.'
          },
          q2: {
            q: 'The request failed with pricing_unavailable or a missing price?',
            a: 'The model has no valid price right now, and the platform rejects the request before the upstream call, following the no-free-ride rule for missing prices. This is a price configuration issue on the platform side; please report the model name and the timestamp to the site owner; they can check it in the admin missing-price list and price overview.'
          },
          q3: {
            q: 'The request was rate limited (429)?',
            a: 'The group QPS limit or your concurrency limit was reached. You can see your current concurrency on the API Keys page; switch groups if needed, or ask the site owner to raise the limit.'
          },
          q4: {
            q: 'The balance did not arrive?',
            a: 'There are two cases. For Recharge / Subscription, the self-hosted payment credits the balance as soon as payment succeeds; if it did not arrive, give the site owner the order number. For the white-label Recharge Center, a successful payment does not credit the balance automatically: you copy the card key and redeem it on the Redeem page. That entry is not enabled yet, so the page simply says the top-up service is not enabled.'
          },
          q5: {
            q: 'The card key fails on the Redeem page?',
            a: 'The Redeem page currently only accepts codes issued by this site. Card keys from external channels are not connected to the site redemption table yet, so send the card key and the site order number to the site owner. The failure message tells the reasons apart: this code has already been used / the code does not exist / the code has expired / the code is invalid.'
          },
          q6: {
            q: 'I was charged but cannot find the record?',
            a: 'Filter usage records by time window and key first; image and video tasks are asynchronous, so you may need to wait for the task to finish or poll the task status.'
          },
          q7: {
            q: 'Can I get a refund or a price change?',
            a: 'Credit from a top-up is an in-site balance; prices and multipliers are maintained by the platform, and the platform does not adjust sale prices automatically with exchange rates. Corrections to historical billing are handled by the site owner through the reconciliation process.'
          },
          q8: {
            q: 'What should I give the site owner to investigate?',
            a: 'Provide the site order number (for top-up issues), the request_id or the time window plus model name plus the last characters of the key (for billing questions), and the error code with the original message (for call failures). Never send the full key.'
          }
        }
      },

      contact: {
        title: 'What to give the site owner',
        intro: 'The more specific the information, the faster the investigation.',
        tableHead: { t: 'Situation', d: 'Required' },
        rows: {
          topup: { t: 'Top-up / card key', d: 'Site order number and payment time.' },
          billing: { t: 'Billing question', d: 'request_id, or the time window plus model name plus the last characters of the key.' },
          failure: { t: 'Call failure', d: 'Error code, full message, model name, client and version.' },
          quota: { t: 'Quota / concurrency', d: 'User ID and the expected value.' }
        },
        note: 'Do not send the full key; check that the key does not appear in full before sharing a screenshot.'
      }
    },

    admin: {
      pricing: {
        title: 'Price sync and the missing-price red line',
        intro: 'The four price layers, the sync cycle, and the rule that must not be relaxed.',
        bullets: {
          layers:
            'Four layers: source price (currency and unit preserved) → base Credit price (digits mapped one-to-one, no conversion) → group multiplier (the commercial pricing lever) → final price (base price × effective multiplier of the group that actually served the request). The top-up ratio is fixed at ¥1 = 1 Credit and does not float with exchange rates.',
          cnySource:
            'Domestic CNY price source: the "use CNY price source for domestic models" switch lives under Feature switches → model pricing and quota billing. Domestic models use the raw CNY price sheet while Claude / GPT and others keep using the USD sheet; ¥1 and $1 both deduct 1 unit of credit, with no conversion. Domestic models missing a CNY price need a manual price. The switch takes effect immediately after saving.',
          syncCycle:
            'Sync cycle: "pricing sync interval (hours)" accepts 1 to 720. Saving re-arms the timer immediately without a restart, and the page shows the effective interval. The sync status card shows the model count and last update time for USD and CNY separately, plus the cny_error, usd_error and deepseek_calendar_error hints, refreshes every 5 seconds, and offers "sync now" and "retry".',
          lastGood:
            'Failures do not overwrite a valid price: a failed sync does not overwrite the Last Known Good Price, keeps serving on the most recent valid price, records the error, and merges or suppresses repeated notifications.',
          currencyGuard:
            'Currency and unit anomalies must not silently overwrite: when CNY data shows USD, USD data shows CNY, or a unit mapping looks wrong, the current valid price is kept.',
          missing:
            'Missing-price red line: an unpriced model is rejected before the upstream call (pricing_unavailable). Zero-cost pass-through is not allowed and there is no switch to enable it; media models are not allowed to fall back either. An explicit zero price settles at 0 as a normal price, which is a different thing from a missing price.',
          missingList:
            'Missing-price list and price overview: the admin channel pricing page has three views, channel list / price overview / missing-price list. The price overview is a read-only summary, and the missing-price list probes price completeness model by model against the current default price card and links to the pricing editor.',
          missingListLimit:
            'Neither view is an authoritative site-wide price catalogue: the default price card only offers a single-model query, so the price overview lists only the models appearing in channel and group rules; the probe results may differ from actual billing, and wildcard rules are not probed per model.',
          peak:
            'Peak/off-peak detection: the official DeepSeek price card uses Asia/Shanghai, with 09:00-12:00 and 14:00-18:00 on weekdays priced at 2x and everything else at 1x; the public-holiday calendar takes priority and weekends (including make-up workdays) are off-peak. A group with its own peak window uses that configuration. The holiday calendar is maintained per year in the pricing settings, and a year with neither a custom nor a built-in calendar has the official peak surcharge disabled.'
        }
      },

      scheduling: {
        title: 'Scheduling strategy',
        intro: 'Global strategy, group overrides and failure classification.',
        bullets: {
          global:
            'Global: under gateway scheduling settings you can pick stability-first, balanced, or response-speed-first; a group can instead pick "inherit global" and shows the currently effective strategy. The priority order is global strategy → group default (inherit or override) → live account state → final scheduling.',
          budget:
            'Budget: "max sends per request" and "total budget per request (seconds)" default to 16 sends and 600 seconds, covering all real upstream retries; smaller pre-existing limits still apply.',
          ungrouped:
            'Ungrouped keys: keeping "allow scheduling of ungrouped keys" off is recommended, so every key clearly belongs to a group; with it off, an ungrouped key gets a 403.',
          threshold:
            'Auto-threshold: "platform account auto-stop threshold" temporarily removes an account from scheduling once its native usage window reaches the given percentage, where 100 disables the account; there is a global default plus per-account overrides.',
          fault:
            'Failure classification: an invalid upstream account or key, an account rate limit, an account at capacity, and transient connection or upstream failures first switch account, and switch group once the group runs out of candidates. An invalid site key, insufficient balance, bad parameters, an invalid model name, and content-moderation or business rejections are returned directly with no account or group switch. When a timeout makes it impossible to confirm whether the upstream ran, the request is not blindly resent; idempotency or a query is preferred. A request that already streamed output is not replayed, and submitted image or video tasks are not downgraded into text retries.'
        }
      },

      concurrency: {
        title: 'Concurrency troubleshooting rules',
        intro:
          'The concurrency count is a hard limit, acquired and released atomically, and a value of 0 or less means unlimited. The load factor is the denominator used when picking accounts, and falls back to the concurrency count when left empty. The two fields mean different things, so do not mix them up.',
        bullets: {
          r1: 'Load factor greater than the concurrency count makes the load rate look too low, so accounts stay pinned at their limit.',
          r2: 'Load factor smaller than the concurrency count makes the load rate look too high, so accounts get filtered out and appear to run no concurrency at all.',
          r3: 'A long sticky-session TTL combined with a high sticky weight concentrates traffic on a few accounts.',
          r4: 'Realtime audio (WS) long connections occupy concurrency slots too.',
          r5: 'The slot TTL (30 minutes by default) keeps a slot occupied for a long time when a request exits abnormally.',
          r6: 'Account snapshots lag by seconds (outbox polling), so wait one refresh cycle before judging a concurrency change.'
        }
      },

      reconciliation: {
        title: 'Reconciliation and troubleshooting',
        intro: 'How the three common anomalies are judged and handled.',
        tableHead: { t: 'Symptom', d: 'Judgement and handling' },
        rows: {
          mismatch: {
            t: 'Recorded group or spend disagrees with the price-lock snapshot',
            d: 'The ledger and the snapshot diverge, and the page only presents both stored values without correcting anything. Stop the user from resubmitting, reconcile the ledger with request_id and the time window, and correct through the reconciliation process if needed.'
          },
          missingRow: {
            t: 'Charged, but no usage record line',
            d: 'An anomaly between writing the log and settling the charge. The usage_billing ledger is authoritative; a failed Live settlement keeps a pending status and retries automatically, and the retry amount stays fixed instead of growing over time.'
          },
          rejected: {
            t: 'Request rejected but nothing charged',
            d: 'Missing price, authentication, balance and parameter errors neither switch accounts nor settle, which is expected behaviour. Fix the price table before ramping traffic.'
          }
        },
        note: 'Key quota and window limits accumulate atomically in the unified ledger, and a database trigger drives the auth-cache invalidation channel after a change. After manually changing a key quota or group, the cache can take one refresh cycle to take effect, so wait a cycle before concluding that the change did not apply.'
      }
    },

    paths: {
      title: 'Where things live',
      intro: 'Check here when you cannot find an entry in the UI.',
      tableHead: { t: 'Feature', d: 'Path' },
      rows: {
        keys: { t: 'API keys', d: '/keys' },
        usage: { t: 'Usage records', d: '/usage' },
        keyUsage: { t: 'Key usage', d: '/key-usage' },
        purchase: { t: 'Recharge / Subscription (currently available)', d: '/purchase' },
        shop: { t: 'Recharge Center (white-label products, not enabled yet)', d: '/shop' },
        orders: { t: 'My orders', d: '/orders' },
        redeem: { t: 'Redeem', d: '/redeem' },
        videoTasks: { t: 'Video tasks', d: '/video-tasks' },
        batchImage: { t: 'Batch images', d: 'Sidebar "Batch Images"' }
      }
    }
  }
}
