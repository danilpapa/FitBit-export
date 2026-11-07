# FitBit Export

A small Go CLI that reads your Fitbit data from the Google Health API and prints a daily JSON report. The report covers sleep, recovery, activity, and workouts. A separate `strength` command adds strength workouts, weight, and nutrition.

You can also run it every day in a Claude Code scheduled task. Claude reads the report and sends you short advice in Telegram.

## Requirements

- Go 1.25 or later
- A Fitbit device linked to your Google account
- A Google Cloud project

For daily advice, you also need:

- A Claude plan that includes Claude Code scheduled tasks
- A GitHub copy of this repository
- A Telegram bot

## Set up Google access

1. In Google Cloud Console, create a project and enable the Google Health API.
2. Open Google Auth Platform and set the audience to **External**.
3. Add these scopes on the **Data Access** page:

   ```text
   https://www.googleapis.com/auth/googlehealth.activity_and_fitness.readonly
   https://www.googleapis.com/auth/googlehealth.health_metrics_and_measurements.readonly
   https://www.googleapis.com/auth/googlehealth.nutrition.readonly
   https://www.googleapis.com/auth/googlehealth.sleep.readonly
   ```

4. Add your Google account as a test user.
5. Create an OAuth client of type **Web application**.
6. Add this redirect URI to the client:

   ```text
   http://localhost:8085/callback
   ```

7. Create a `.env` file in the project root:

   ```env
   GOOGLE_HEALTH_CLIENT_ID=your-client-id
   GOOGLE_HEALTH_CLIENT_SECRET=your-client-secret
   ```

8. Sign in. This opens your browser and saves `GOOGLE_HEALTH_REFRESH_TOKEN` to `.env`:

   ```bash
   go run ./cmd/fitbit-export auth
   ```

While the app status is **Testing**, the refresh token expires after 7 days. To avoid this, publish the app on the **Audience** page. Google may ask for a home page, privacy policy, and authorized domain before you can publish.

The nutrition scope is restricted, so Google shows a verification notice. If only you use the app, you do not need verification: click **Advanced** on the warning screen when you sign in.

## Use the CLI

Run `make` in the project root to see short commands for everything below, for example `make strength` or `make note TEXT="Slept badly"`.

Print today's report:

```bash
go run ./cmd/fitbit-export
```

Add a note for today:

```bash
go run ./cmd/fitbit-export note "Slept badly and felt tired"
```

Notes are saved locally in `exports/notes/` and added to the report. Cloud runs cannot see them.

Print your recent strength workouts:

```bash
go run ./cmd/fitbit-export strength
```

To track a workout, add what you did to the workout notes in Google Health as plain text, for example:

```text
Bench Press (Barbell)
Set 1: 80 kg x 12
Set 2: 90 kg x 6
```

If you log workouts in Hevy, you can paste only the share link instead, for example `https://hevy.com/workout/48nSfXwj9Tv`. The `strength` command opens the public page in headless Chrome and puts the sets in place of the link. This needs Google Chrome or Chromium on the machine. Notes that already contain sets are left as they are.

The record must be one that returns `notes` from the API, and its date must match the workout. The command returns your last 6 strength workouts with notes from the past 90 days, your latest weight, and today's nutrition. The `strength-coach` skill reads the text and does the analysis.

## Read nutrition from FatSecret (optional)

By default, nutrition comes from Google Health. If you log food in FatSecret instead, the `strength` command can read your daily totals from the FatSecret food diary.

1. Sign up at [platform.fatsecret.com](https://platform.fatsecret.com) and create an app. The free Basic edition is enough.
2. Make sure your FatSecret mobile app uses a fatsecret.com account with an email and password.
3. Add the app keys to `.env`:

   ```env
   FATSECRET_CONSUMER_KEY=your-consumer-key
   FATSECRET_CONSUMER_SECRET=your-consumer-secret
   ```

4. Link your account. This opens FatSecret in your browser. Allow access, copy the verification code, and paste it in the terminal. The command saves `FATSECRET_ACCESS_TOKEN` and `FATSECRET_ACCESS_SECRET` to `.env`:

   ```bash
   go run ./cmd/fitbit-export fatsecret-auth
   ```

5. Run `go run ./cmd/fitbit-export strength` and check `nutrition_today`.

When all four FatSecret values are set, `strength` reads nutrition from FatSecret and weight from Google Health.

## Set up daily advice

1. Fork this repository.
2. Create a Telegram bot with [@BotFather](https://t.me/BotFather) and send it a message.
3. Open `https://api.telegram.org/bot<TOKEN>/getUpdates` and copy your `chat.id`.
4. Create a cloud environment at [claude.ai/code](https://claude.ai/code).
5. Set **Network access** to **Custom**, turn on the default package manager list, and allow these domains:

   ```text
   health.googleapis.com
   oauth2.googleapis.com
   api.telegram.org
   ```

6. Add these environment variables:

   ```env
   GOOGLE_HEALTH_CLIENT_ID=...
   GOOGLE_HEALTH_CLIENT_SECRET=...
   GOOGLE_HEALTH_REFRESH_TOKEN=...
   TELEGRAM_BOT_TOKEN=...
   TELEGRAM_CHAT_ID=...
   ```

   If you use FatSecret, also allow `platform.fatsecret.com` and add the four `FATSECRET_*` values from `.env`.

   If you paste Hevy links, also allow `hevy.com`, `api.hevyapp.com`, and `d2l9nsnmtah87f.cloudfront.net`. The cloud image already has Google Chrome.

7. Create a private repository named `fitbit-memory`. Never make this repository public. The skills keep short summaries there, plus an archive of daily reports, sent advice, analyzed workouts, and daily nutrition.
8. Add `profile.md` to the root of `fitbit-memory` with your goals, training cycle, preferences, and injuries, for example:

   ```markdown
   # Profile
   ## Goals
   - Build muscle and strength.
   ## Training cycle
   1. Chest and biceps
   2. Back and triceps
   3. Legs and shoulders
   ```

9. Create a daily scheduled task at [claude.ai/code/routines](https://claude.ai/code/routines).
10. Select both repositories and the new cloud environment, then remove any connectors you do not need.
11. Use this prompt:

   ```text
   Run the fitbit-advice skill, then the strength-coach skill, then the nutrition-coach skill.
   ```

12. Click **Run now** and check that the message arrives in Telegram. The `strength-coach` skill sends a message only on days with a logged strength workout. The `nutrition-coach` skill reviews your food every day and links it to your training.

## Customize

- **Goals and training cycle:** edit `profile.md` in `fitbit-memory`. Anything you add to the task prompt takes priority over it.
- **Schedule:** change the task time. Run it late in the evening so the report covers most of the day.
- **Advice:** edit `.claude/skills/fitbit-advice/SKILL.md`.
- **Strength coaching:** edit `.claude/skills/strength-coach/SKILL.md`.
- **Nutrition coaching:** edit `.claude/skills/nutrition-coach/SKILL.md`.
- **Metrics:** edit `recoveryMetrics` and `activityMetrics` in `internal/report/report.go`.
- **Public pages:** if you use GitHub Pages for Google publishing, replace the text and links in `docs/`.

## Troubleshooting

- **`OAuth refresh returned HTTP 400`:** the refresh token has expired, or the run cannot see your variables. Check that the task uses the right cloud environment, then run `auth` again if needed.
- **Telegram returns `403` in a cloud run:** allow `api.telegram.org` in the environment used by the task.
- **`nutrition` is in `unavailable`:** add the nutrition scope, run `auth` again, and update the refresh token in the cloud environment.
- **`hevy` is in `unavailable`:** a Hevy page did not open. Check the Hevy domains in the cloud environment and look for `hevy:` lines in the log.
- **FatSecret error in the log:** error `9` means the access token is no longer valid, so run `fatsecret-auth` again. Error `21` means FatSecret rejected the IP address of the run.

## Privacy

Your health data goes to Anthropic and Telegram when you use daily advice. The CLI only reads your FatSecret diary and never changes it. Never commit `.env`, reports, or personal health data to a public repository.

The advice is not medical advice.
