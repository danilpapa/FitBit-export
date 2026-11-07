# FitBit Export

A Go CLI that reads your Fitbit data from the Google Health API. A daily Claude Code task turns it into advice on recovery, strength training, and nutrition, and sends it to Telegram.

You need [Go 1.25+](https://go.dev/dl/), a Fitbit linked to Google, a Claude plan with scheduled tasks, and a [fork of this repository](https://github.com/danilpapa/FitBit-export/fork). Run `make` to list all commands.

## Setup

**1. Google Cloud**

1. [Create a project](https://console.cloud.google.com/projectcreate) and enable the [Google Health API](https://console.cloud.google.com/apis/library/health.googleapis.com).
2. In Google Auth Platform, set the audience to **External** and add yourself as a test user on [Audience](https://console.cloud.google.com/auth/audience).
3. On [Data Access](https://console.cloud.google.com/auth/scopes), add the scopes `googlehealth.activity_and_fitness.readonly`, `googlehealth.health_metrics_and_measurements.readonly`, `googlehealth.nutrition.readonly`, and `googlehealth.sleep.readonly`.
4. On [Clients](https://console.cloud.google.com/auth/clients), create an OAuth client of type **Web application** with the redirect URI `http://localhost:8085/callback`.
5. Publish the app on [Audience](https://console.cloud.google.com/auth/audience), or the token expires in 7 days. Google asks for a home page, privacy policy, and domain on [Branding](https://console.cloud.google.com/auth/branding): you can use [GitHub Pages](https://docs.github.com/en/pages/getting-started-with-github-pages/configuring-a-publishing-source-for-your-github-pages-site) from `docs/`.

**2. Sign in**

Put `GOOGLE_HEALTH_CLIENT_ID` and `GOOGLE_HEALTH_CLIENT_SECRET` in `.env`, then run `make auth`. On the warning screen, click **Advanced**: verification is not needed for personal use. Check the result with `make report`.

**3. FatSecret (optional, for food)**

1. Sign up at [platform.fatsecret.com](https://platform.fatsecret.com) and copy the keys from **Manage API Keys**. The free Basic edition is enough.
2. Put `FATSECRET_CONSUMER_KEY` and `FATSECRET_CONSUMER_SECRET` in `.env`. Use the **OAuth 1.0 Shared Secret**, not the OAuth 2.0 Client Secret.
3. Run `make fatsecret-auth`, allow access, and paste the code into the terminal.

How it works: [documentation/fatsecret.md](documentation/fatsecret.md). Without FatSecret, nutrition comes from Google Health. 

**4. Telegram**

1. Create a bot with [@BotFather](https://t.me/BotFather) and send it any message.
2. Open `https://api.telegram.org/bot<TOKEN>/getUpdates` and copy `chat.id`.
3. Optional: in @BotFather, send `/setcommands` and add `note`, `strength`, and `nutrition`.

**5. Private memory**

1. [Create a private repository](https://github.com/new) `fitbit-memory` with a `profile.md` that lists your goals, training cycle, preferences, and injuries. Never make it public: the skills keep your summaries and archives there.
2. [Install the Claude GitHub App](https://github.com/apps/claude/installations/new) on both repositories, or the task cannot see the private one.

**6. Cloud environment** at [claude.ai/code](https://claude.ai/code) ([docs](https://code.claude.com/docs/en/claude-code-on-the-web))

1. Set **Network access** to **Custom**, keep the package manager list, and allow `health.googleapis.com`, `oauth2.googleapis.com`, and `api.telegram.org`. Add `platform.fatsecret.com` for FatSecret, and `hevy.com`, `api.hevyapp.com`, and `d2l9nsnmtah87f.cloudfront.net` for Hevy.
2. Add the variables from `.env`: three `GOOGLE_HEALTH_*`, four `FATSECRET_*` if you use FatSecret, plus `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID`.

**7. Scheduled task** at [claude.ai/code/routines](https://claude.ai/code/routines) ([docs](https://code.claude.com/docs/en/routines))

1. Run it daily, late in the evening, with both repositories and the new environment.
2. Use this prompt:

   ```text
   Run the fitbit-advice skill, then the strength-coach skill, then the nutrition-coach skill.
   Commit and push changes in fitbit-memory directly to main. Do not commit to FitBit-export.
   ```

3. Click **Run now** and check Telegram.

## Every day

- **Workout:** Paste a Hevy share link. The CLI opens the link in headless Chrome ([documentation/hevy.md](documentation/hevy.md)). The record date must match the workout, and not every record type returns notes through the API: check with `make strength`.
- **Food and weight:** log food in FatSecret or Google Health, and weigh yourself in Google Health.
- **Commands:** send the bot `/note`, `/strength`, or `/nutrition` with text, for example `/nutrition dinner out, do not log today`. They apply to that day only. Check them with `make inbox`.
- **Goals:** edit `profile.md` in `fitbit-memory`. To change how the advice works, edit the skills in `.claude/skills/`.

> Your health data goes to Anthropic and Telegram. This is not medical advice.
