# Coral LLC Launch Plan

**Unified formation, IP, and commercialization plan for the Coral desktop app business.**

**Disclaimer: This plan is for informational purposes only. Consult with a qualified business attorney and CPA before executing. State-specific requirements may vary.**

---

## Overview

**Entity:** Delaware Single-Member LLC
**Formation method:** Stripe Atlas ($500 all-in)
**Timeline to first payment:** ~14 business days
**Total formation cost:** ~$700-900 (Atlas + trademark filing)

---

## Phase 1: Formation (Days 1-5)

### Day 1 — Start LLC Formation

- [ ] **Start Stripe Atlas application** at stripe.com/atlas
  - Select: Delaware LLC
  - Provide: Personal info, business name ("Subgentic.ai" or "Subgentic AI LLC")
  - Cost: $500 (includes: Delaware filing, registered agent 1yr, EIN filing, operating agreement template, Stripe account setup)
  - Processing time: ~2 business days for LLC formation

- [ ] **Reserve the business name** — Atlas checks Delaware name availability during signup. Company name: "Subgentic.ai" (or "Subgentic AI LLC" for the legal entity name)

- [ ] **Simultaneously: Continue HN launch prep** — LLC formation does not block the open-source launch

### Day 2-3 — LLC Approved

- [ ] **Receive Delaware Certificate of Formation** (from Atlas, via email)
- [ ] **Receive EIN confirmation** (Atlas files SS-4 with IRS; EIN issued same-day or within 1-2 days)
- [ ] **Review and sign Operating Agreement** (Atlas provides a standard single-member LLC template via Cooley LLP)
  - If customization is needed, key additions:
    - IP ownership clause (all IP created for the company belongs to the company)
    - Single-member management provisions
    - Distribution and profit allocation language

### Day 3-4 — Bank Account

- [ ] **Open Mercury business bank account** (Atlas provides direct access)
  - Required documents: Certificate of Formation, EIN letter, Operating Agreement, personal ID
  - Mercury approval: typically 1-2 business days
  - No minimum balance, no monthly fees

- [ ] **Alternative banks:** Relay (also free, multi-account), Chase (if you prefer a traditional bank — $15/mo fee)

### Day 5 — Financial Infrastructure Active

- [ ] **Stripe account activated** (Atlas sets this up automatically)
  - Connect to Mercury bank account for payouts
  - Stripe credits from Atlas: $2,500 in product credits + up to 1 year free processing on first $100K

- [ ] **Stripe Billing setup:**
  - Create Product: "Coral Pro" — $19/month (or $190/year)
  - Create Product: "Coral Team" — $39/user/month
  - Create Product: "Coral Enterprise" — custom pricing (contact form)
  - Configure Stripe Checkout for each product
  - Set up Stripe Customer Portal for self-service subscription management
  - Enable Stripe Tax for automatic sales tax calculation

**Phase 1 Complete: LLC formed, bank open, Stripe accepting payments.**

---

## Phase 2: IP Assignment (Days 3-7)

### Day 3 — Execute IP Assignment Agreement

- [ ] **Draft IP Assignment Agreement** (Legal Advisor to prepare)
  - Assigns ALL Coral IP from the individual founder to the LLC:
    - Copyright in all source code, documentation, templates, static assets
    - Patent rights (provisional application + future filings)
    - Trademark rights ("Coral" name, any logos, domain names)
    - Trade secrets (deployment configs, business plans, customer data)
    - All derivative works and future improvements
  - Include consideration clause ($10 or other nominal value — required for a valid assignment)
  - Both parties sign (founder as individual AND as LLC manager)

- [ ] **Save executed copy** in company records (digital + physical backup)

### Day 4-5 — Update Public Records

- [ ] **Update LICENSE file** — Change copyright holder to "Subgentic.ai"
- [ ] **Update README.md** — Update any copyright notices
- [ ] **Update landing page** — Update footer copyright to company name
- [ ] **Update pyproject.toml** — Update author/maintainer to company name if desired

### Day 5-7 — Record Patent Assignment

- [ ] **File Assignment with USPTO** (Form PTO/AIA/96)
  - Records the transfer of patent rights from individual to LLC
  - Fee: $40 (electronic filing via EPAS at assignments.uspto.gov)
  - Required info: Provisional application number, assignor name, assignee name (LLC), date of assignment, execution date
  - Processing time: 2-4 weeks (but effective immediately upon filing)

**Phase 2 Complete: All IP owned by the LLC with proper documentation.**

---

## Phase 3: Patent Filing (Days 5-10)

### Day 5-7 — File Provisional Patent Application

- [ ] **Engage a patent attorney** (if not already)
  - Look for: software patent experience, Alice/101 expertise, open-source familiarity
  - Estimated cost: $2,000-5,000 for provisional drafting + filing
  - Our draft (docs/patents/utility_patent_provisional.md) provides a strong starting point

- [ ] **File U.S. Provisional Patent Application**
  - Filed in the name of the LLC (as assignee)
  - Inventor: individual founder (required — companies can't be inventors)
  - USPTO small entity fee: $1,600
  - Include: specification, claims, abstract, drawings (from figure_requirements.md)

- [ ] **Once filed: Add "Patent Pending" to marketing materials**
  - Landing page
  - README.md
  - Desktop app About screen
  - Any printed materials

### Day 7-10 — Trademark Application

- [ ] **Search USPTO TESS database** for "Coral" conflicts in Class 9 (software) and Class 42 (SaaS)
  - trademarks.uspto.gov — search for "Coral" in relevant classes
  - Common word, likely some conflicts — attorney can advise on distinctiveness

- [ ] **File trademark application** (if search is clear)
  - File in: Class 9 (computer software) and Class 42 (software as a service)
  - Filing basis: Use in commerce (if already selling) or Intent to Use (if pre-revenue)
  - Fee: $250 per class ($500 total for two classes) via TEAS Plus
  - Processing time: 8-12 months to registration

**Phase 3 Complete: Patent pending, trademark application filed.**

---

## Phase 4: Legal Documents (Days 7-14)

### Day 7-10 — Privacy Policy

- [ ] **Draft Privacy Policy** (Legal Advisor to prepare)
  - Must cover: what data is collected, how it's used, who it's shared with, user rights
  - GDPR compliant (if any EU users): lawful basis, data subject rights, DPO contact
  - CCPA compliant (if any CA users): right to know, delete, opt-out of sale
  - Key sections for Coral desktop app:
    - License key validation (sends key to server for verification)
    - Crash reporting / telemetry (if any — specify what's collected)
    - Payment processing (Stripe handles PCI compliance, but disclose the relationship)
    - Session data: clarify that agent sessions, code, and prompts are LOCAL ONLY and never transmitted
  - Host at: coral.dev/privacy or GitHub Pages

### Day 10-12 — Terms of Service

- [ ] **Draft Terms of Service** (Legal Advisor to prepare)
  - Software license grant (what users can/can't do with Pro/Team/Enterprise)
  - Acceptable use policy
  - Subscription terms: billing, renewal, cancellation
  - Refund policy: 14-day money-back guarantee (standard for dev tools)
  - Limitation of liability and warranty disclaimer
  - Governing law: Delaware
  - Dispute resolution: arbitration or small claims (avoid expensive litigation)
  - Termination: conditions under which access can be revoked
  - Open-source acknowledgment: clarify relationship between Apache 2.0 core and proprietary desktop features
  - Host at: coral.dev/terms or GitHub Pages

### Day 12-14 — Additional Documents

- [ ] **EULA for desktop app** (End User License Agreement)
  - Displayed during desktop app installation
  - Covers: license scope, restrictions, termination, updates
  - Shorter than full ToS — focused on the desktop app specifically

- [ ] **Contributor License Agreement (CLA)** — Optional but recommended
  - Ensures external contributors grant IP rights for their contributions
  - Apache 2.0 includes a built-in patent grant, but a CLA provides additional protection
  - Options: CLA Assistant (GitHub bot) for automated CLA signing
  - Can be deferred until external contributions become significant

**Phase 4 Complete: All legal documents in place for commercial sales.**

---

## Phase 4B: License Key & Payment Infrastructure (Days 10-14)

*Added by Business Development — covers the monetization plumbing needed before first sale.*

### License Key Management (Keygen.sh — $49/mo)

- [ ] **Create Keygen.sh account** (keygen.sh) — Indie plan at $49/mo
- [ ] **Define license policy:**
  - Pro: 1 license key, activate on up to 3 machines
  - Team: 1 license key per seat, admin can manage seats
  - Trial: 14-day expiring key, no credit card required
- [ ] **Connect Stripe webhook to Keygen:**
  - `checkout.session.completed` → create license key
  - `customer.subscription.deleted` → suspend license key
  - `customer.subscription.updated` → update license tier
  - `invoice.payment_failed` → send grace period warning (7 days)

### Purchase Flow (End-to-End)

```
Landing Page "Start Pro Trial" → Email capture form → Keygen creates 14-day trial key
  → Key emailed via SendGrid/Resend → User enters key in desktop app → App validates → Pro unlocked

Landing Page "Buy Pro" → Stripe Checkout ($19/mo) → Payment succeeds
  → Stripe webhook → Keygen creates permanent key → Key emailed → App validates → Pro unlocked

Desktop App "Trial Expired" → In-app upgrade prompt → Stripe Checkout → Same flow as above
```

### Pricing Page Implementation

- [ ] **Stripe Checkout links** for each tier (embeddable on landing page)
- [ ] **Stripe Customer Portal** link for existing subscribers (manage/cancel/update payment)
- [ ] **Annual discount toggle** on pricing section (show monthly vs annual pricing)
- [ ] **Team seat selector** (quantity picker that adjusts total before checkout)

### Refund Policy

- 14-day money-back guarantee, no questions asked
- Process via Stripe dashboard (refund → Keygen webhook suspends key)
- After 14 days: pro-rated refund at operator's discretion
- Publish refund policy on landing page footer + Terms of Service

---

## Phase 4C: Go-to-Market Coordination (Parallel with Phases 1-4)

*Added by Business Development — maps the marketing launch plan to the LLC formation timeline.*

### How the Two Tracks Run in Parallel

| Day | LLC/Legal Track (this plan) | Marketing Track (30-day launch plan) |
|-----|---------------------------|--------------------------------------|
| 1 | Stripe Atlas application | Landing page final polish |
| 2 | LLC processing | Demo GIF/video recording |
| 3 | LLC formed, EIN pending | Show HN post finalized |
| 4 | IP Assignment executed | **HN LAUNCH** (no entity needed) |
| 5 | Bank account opens | Reddit blitz begins |
| 6 | Stripe verified | Twitter/X thread |
| 7 | Entity fully operational | Feedback triage |
| 8-9 | Patent filing | Blog Post #1 |
| 10 | "Patent Pending" live | Discord server launch |
| 11-12 | Privacy Policy + ToS drafted | YouTube demo |
| 13-14 | Legal docs published | Product Hunt prep |
| 15 | Stripe products created | **Product Hunt launch** |
| 16-17 | License key system live | Blog Post #2 |
| 18-21 | Full payment flow tested | Partnership outreach |
| 22-30 | First sales accepted | Desktop beta invites |

### Key Principle
The marketing launch (HN, Reddit, etc.) drives awareness for the FREE open-source project. No money changes hands until Week 3-4, by which time the LLC, Stripe, and license system are all operational. The two tracks are independent — neither blocks the other.

### Success Metrics (30-Day Targets)

| Metric | Target | Stretch |
|--------|--------|---------|
| GitHub Stars | 1,000 | 2,500 |
| PyPI Installs | 500 | 1,500 |
| Discord Members | 100 | 300 |
| Landing Page Visits | 10,000 | 25,000 |
| Desktop Beta Signups | 200 | 500 |
| Newsletter Mentions | 2 | 5 |
| First Paying Customer | 1 | 10 |

### Break-Even Analysis

| Scenario | Monthly Revenue | Annual Revenue | Break-Even? |
|----------|----------------|----------------|-------------|
| 15 Pro subs | $285/mo | $3,420/yr | Covers formation + recurring costs |
| 30 Pro subs | $570/mo | $6,840/yr | Covers all costs including patent |
| 5 Team subs (5 seats each) | $975/mo | $11,700/yr | Profitable + funds development |
| 50 Pro + 3 Teams (5 seats) | $1,535/mo | $18,420/yr | Sustainable solo business |

---

## Phase 5: Go-Live (Days 14+)

### Day 14 — First Sale Ready

- [ ] **Verify complete checklist:**
  - LLC formed and active
  - EIN obtained
  - Business bank account open
  - Stripe accepting payments
  - IP assigned to LLC
  - Patent provisional filed (or in progress)
  - Trademark application filed (or in progress)
  - Privacy Policy published
  - Terms of Service published
  - Landing page updated with legal links
  - Desktop app license key system functional

- [ ] **Enable payment links on landing page**
- [ ] **Begin desktop app beta invites** (from waitlist)
- [ ] **Monitor first transactions** — verify Stripe → bank flow works

### Ongoing (Monthly)

- [ ] **Bookkeeping** — Track all income/expenses (Wave free, or QuickBooks $30/mo)
- [ ] **Quarterly estimated taxes** — File Form 1040-ES if profitable (due Apr 15, Jun 15, Sep 15, Jan 15)
- [ ] **Annual:** Delaware franchise tax ($300/yr), registered agent renewal ($150/yr via Atlas)
- [ ] **Annual:** Renew home state foreign LLC registration (if applicable)

---

## Cost Summary

| Item | Cost | When |
|------|------|------|
| Stripe Atlas (LLC + EIN + agent + Stripe) | $500 | Day 1 |
| Home state foreign LLC registration | $100-200 | Day 5-7 (if required) |
| Patent attorney (provisional drafting + filing) | $2,000-5,000 | Days 5-10 |
| USPTO provisional patent fee (small entity) | $1,600 | Days 5-10 |
| USPTO trademark filing (2 classes) | $500 | Days 7-10 |
| USPTO patent assignment recording | $40 | Days 5-7 |
| **Total initial costs** | **$4,740-7,840** | |
| | | |
| **Recurring annual costs:** | | |
| Delaware franchise tax | $300/yr | |
| Registered agent | $150/yr | |
| Home state foreign LLC (if applicable) | $100-200/yr | |
| Stripe processing | 2.9% + $0.30/txn | |

---

## Decision Log

| Decision | Recommendation | Rationale |
|----------|---------------|-----------|
| Entity type | Delaware Single-Member LLC | Pass-through tax, simple admin, easy conversion to C-Corp later |
| Formation method | Stripe Atlas | All-in-one package, fastest path to accepting payments |
| LLC timing | This week (parallel with HN launch) | Liability protection + clean IP chain from day one |
| HN launch timing | Do NOT wait for LLC | Open-source launch has negligible legal risk |
| S-Corp election | Defer until >$50K profit | Adds payroll complexity; not worthwhile at low revenue |
| C-Corp conversion | Defer until VC interest | No need for corporate structure while bootstrapping |
| CLA for contributors | Defer until significant external contributions | Apache 2.0 patent grant provides baseline protection |
| Desktop app EULA | Before first desktop sale | Required for proprietary feature licensing |

---

## Checklist (Copy-Paste Tracker)

```
FORMATION
[ ] Start Stripe Atlas application
[ ] LLC Certificate of Formation received
[ ] EIN confirmed
[ ] Operating Agreement signed
[ ] Business bank account opened (Mercury)
[ ] Stripe account active and connected to bank

IP ASSIGNMENT
[ ] IP Assignment Agreement drafted
[ ] IP Assignment Agreement executed
[ ] LICENSE file updated with company name
[ ] Patent assignment recorded with USPTO (Form PTO/AIA/96)

PATENT & TRADEMARK
[ ] Patent attorney engaged
[ ] Provisional patent application filed
[ ] "Patent Pending" added to marketing materials
[ ] Trademark search completed (USPTO TESS)
[ ] Trademark application filed (Class 9 + 42)

LEGAL DOCUMENTS
[ ] Privacy Policy drafted and published
[ ] Terms of Service drafted and published
[ ] Desktop app EULA drafted
[ ] Refund policy defined (14-day recommendation)

FINANCIAL
[ ] Stripe Billing products created (Pro, Team, Enterprise)
[ ] Stripe Checkout configured
[ ] Stripe Customer Portal enabled
[ ] Stripe Tax enabled
[ ] Keygen.sh account created (Indie plan, $49/mo)
[ ] License policies defined (Pro, Team, Trial)
[ ] Stripe-to-Keygen webhook connected
[ ] Trial key flow tested (email → key → app activation)
[ ] Purchase flow tested (Stripe Checkout → key delivery → app activation)
[ ] Refund flow tested (Stripe refund → key suspension)
[ ] First test transaction completed

GO-LIVE
[ ] Landing page legal links added (Privacy, Terms)
[ ] Payment links enabled
[ ] Desktop beta invites sent
[ ] First real payment received
[ ] Bookkeeping system set up
```
