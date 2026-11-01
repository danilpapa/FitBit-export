# FitBit Export

A small Go CLI that reads your Fitbit data from the Google Health API and prints a daily JSON report. The report covers sleep, recovery, activity, and workouts.

You can also run it every day in a Claude Code scheduled task. Claude reads the report and sends you short advice in Telegram.

## Requirements

- Go 1.24 or later
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

## Use the CLI

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

To track a workout, add its public Hevy link to the workout notes in Google Health, for example `https://hevy.com/workout/abc123`. The record must be one that returns `notes` from the API, and its date must match the workout. The command opens each link in headless Chrome, reads the sets, and calculates volume and estimated one-rep max. It covers the last 12 linked workouts from the past 90 days.

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
   dl.google.com
   hevy.com
   api.hevyapp.com
   ```

6. Add this setup script. It installs Chrome for the `strength` command:

   ```bash
   #!/bin/bash
   curl -fsSL -o /tmp/chrome.deb https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb
   apt-get install -y /tmp/chrome.deb
   ```

7. Add these environment variables:

   ```env
   GOOGLE_HEALTH_CLIENT_ID=...
   GOOGLE_HEALTH_CLIENT_SECRET=...
   GOOGLE_HEALTH_REFRESH_TOKEN=...
   TELEGRAM_BOT_TOKEN=...
   TELEGRAM_CHAT_ID=...
   ```

8. Create a daily scheduled task at [claude.ai/code/routines](https://claude.ai/code/routines).
9. Select your repository and the new cloud environment, then remove any connectors you do not need.
10. Use this prompt:

   ```text
   Run the fitbit-advice skill, then run the strength-coach skill.
   My goals: <your goals>.
   My body weight: <kg>.
   ```

11. Click **Run now** and check that the message arrives in Telegram. The `strength-coach` skill sends a second message only on days with a linked workout.

## Customize

- **Goals:** edit the prompt of your scheduled task.
- **Schedule:** change the task time. Run it late in the evening so the report covers most of the day.
- **Advice:** edit `.claude/skills/fitbit-advice/SKILL.md`.
- **Strength coaching:** edit `.claude/skills/strength-coach/SKILL.md`.
- **Metrics:** edit `recoveryMetrics` and `activityMetrics` in `internal/report/report.go`.
- **Public pages:** if you use GitHub Pages for Google publishing, replace the text and links in `docs/`.

## Troubleshooting

- **`OAuth refresh returned HTTP 400`:** the refresh token has expired, or the run cannot see your variables. Check that the task uses the right cloud environment, then run `auth` again if needed.
- **Telegram returns `403` in a cloud run:** allow `api.telegram.org` in the environment used by the task.
- **`chrome is not installed`:** check the setup script and allow `dl.google.com`.
- **A workout has an `error`:** open the Hevy link and check that the page is public. Hevy may also have changed its page layout.

## Privacy

Your health data goes to Anthropic and Telegram when you use daily advice. Never commit `.env`, reports, or personal health data to a public repository.

The advice is not medical advice.
