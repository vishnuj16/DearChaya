# **Project Specification: Dear Chaya — Web Showcase**

A minimal, mobile-first, single-story reading experience web application for the short story **"Dear Chaya"** by Vishnu J. (pen name: Vishnu Vyas).

# **1\. Project Overview**

The Dear Chaya Web Showcase provides an intimate, tactile, personal diary reading experience. It combines physical-diary aesthetics with modern web capabilities, including an interactive reader feedback and comment section, a concealed author administration system for real-time content and moderation controls, and light-weight telemetry for reader engagement analytics.

# **2\. Technical Architecture & Stack**

| Component | Technology Selection | Implementation Details |
| :---- | :---- | :---- |
| **Backend & Runtime** | Go (Golang) | Deployed on Vercel Functions using the `/api` directory pattern. |
| **Templating Engine** | Templ | Type-safe Go HTML templating engine compiled directly to Go code. |
| **Client Interactivity** | Alpine.js | Minimal (\~7 KB) reactive framework loaded via CDN. |
| **Styling Framework** | Tailwind CSS | Utility-first CSS framework processed and bundled for optimized CDN deployment. |
| **Data Storage** | SQLite (`app.db`) | Local persistent file-based storage with optional Turso cloud SQLite compatibility. |
| **Analytics Engine** | Google Analytics 4 | GA4 script integrated with custom events for scroll depth and dwell time. |
| **Hosting Platform** | Vercel | Hosted entirely on Vercel Serverless (100% Free Tier compatible). |

# **3\. Visual Aesthetic & Mobile-First UX Guidelines**

## **Theme & Color Palette**

The overall visual theme balances the warmth of a traditional, aged office diary with crisp modern digital typography.

* **Light Theme (Default)**  
  * **Background:** Warm off-white / paper cream (`#FAF7F2`)  
  * **Body Text:** Deep charcoal ink (`#2A2723`)  
  * **Secondary Accent:** Faded vintage navy (`#3B5998`)  
  * **Card / Muted Fill:** Light parchment (`#F0EDE6`)  
* **Dark Theme**  
  * **Background:** Midnight charcoal navy (`#121820`)  
  * **Body Text:** Warm paper off-white (`#E2E8F0`)  
  * **Accent Color:** Soft amber / aged gold (`#D97706`)

## **Typography Hierarchy**

* **Primary Story Body:** Serif font family (*Newsreader*, *Merriweather*, or *Lora*) configured with a generous `1.75` line-height for optimal long-form readability.  
* **Diary Headings & Date Stamps:** Expressive handwriting / casual font (*Caveat* or *Patrick Hand*).  
* **UI Controls & Administration Panel:** Clean, high-legibility sans-serif (*Inter* or system default UI stack).

## **Key Layout Features**

* **Mobile-First Layout:** Centralized narrative container bounded to a maximum reading width of `65ch` (\~650px) on desktop viewports, with dynamic `16px` (`px-5`) padding on mobile screens.  
* **Sticky Navigation Header:** Minimalist glassmorphic top header containing:  
  * Dynamic visual reading progress bar.  
  * Estimated total reading duration indicator (\~4 minutes).  
  * Seamless Light/Dark theme toggle control.  
* **Interactive Story Header:** Animated header transition displaying `Dear Diary,` crossing out or morphing into `Dear Chaya,` upon initial user scroll.

# **4\. Narrative Content Reference**

The full text for "Dear Chaya" is stored separately in the local repository within `story.md`. The server application dynamically renders narrative blocks from either local markdown storage or the active SQLite database instance.

# **5\. Database Schema (SQLite)**

\-- Comments / Reader Reviews table

CREATE TABLE IF NOT EXISTS comments (

    id TEXT PRIMARY KEY,

    author\_name TEXT NOT NULL DEFAULT 'Anonymous',

    content TEXT NOT NULL,

    created\_at TIMESTAMP DEFAULT CURRENT\_TIMESTAMP,

    is\_deleted INTEGER DEFAULT 0

);

\-- Story Content table (Enables live author editing)

CREATE TABLE IF NOT EXISTS story (

    id INTEGER PRIMARY KEY CHECK (id \= 1),

    title TEXT NOT NULL,

    content TEXT NOT NULL,

    updated\_at TIMESTAMP DEFAULT CURRENT\_TIMESTAMP

);

# **6\. Features & Interactive Specification**

## **A. Reader Features**

* **Responsive Narrative Reader:** Story text rendered paragraph-by-paragraph with responsive typography, proper line length, and custom entry dates.  
* **Reader Comment & Feedback Panel:**  
  * **Input Fields:** Name (optional, defaults to "Anonymous") and Comment Body (required textarea).  
  * **Asynchronous Submission:** Dynamic AJAX submit managed via Alpine.js targeting the Go endpoint (`/api/comments`).  
  * **Live Activity Feed:** Dynamic rendering of non-deleted reader comments sorted chronologically with relative timestamps.

## **B. Admin / Author System**

* **Concealed Admin Authentication Entry:**  
  * **Trigger Event:** Triple-clicking the footer copyright string (`©`) or executing the keyboard shortcut `Ctrl + Shift + A`.  
  * **Interface:** Spawns an Alpine.js modal prompt requiring the Author Password.  
* **Backend Authentication Flow:**  
  * Password verification executed server-side via `golang.org/x/crypto/bcrypt` matching against the `ADMIN_PASSWORD_HASH` environment variable.  
  * Successful authentication yields a secure, HTTP-Only session cookie (`admin_token`).  
* **Author Administrative Controls (Active when authenticated):**  
  * **Comment Moderation:** Displays an inline `[Delete]` action next to all reader comments. Invoking soft-deletes (`is_deleted = 1`) or purges the database record.  
  * **Live Narrative Editing:** Unlocks an `[Edit Story]` button in the main header, opening a rich plain-text editor modal to update narrative content directly in SQLite.

## **C. Reader Engagement Analytics (GA4)**

* Global GA4 tag injected into document `<head>`.  
* Automated event listeners tracking scroll depth milestones at 25%, 50%, 75%, 90%, and 100%.  
* Transmits custom telemetry events:  
  * `story_scroll_milestone` containing the specific `percent` integer parameter.  
  * Active active-tab time tracking to measure total reading engagement.

# **7\. Environment Variables (`.env`)**

v  
SQLITE\_DB\_PATH=app.db  
ADMIN\_PASSWORD\_HASH=bcrypt\_hashed\_author\_password  
JWT\_SECRET=your\_random\_secret\_for\_cookies  
GA\_MEASUREMENT\_ID=G-XXXXXXXXXX