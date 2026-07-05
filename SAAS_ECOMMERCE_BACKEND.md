# Dublyobase for SaaS and Ecommerce Backends

Dublyobase is a Postgres-first backend platform for teams that want the speed of PocketBase-style development while keeping their application data in real PostgreSQL. It is designed for products where the database is the source of truth: SaaS dashboards, ecommerce stores, marketplaces, internal tools, customer portals, and AI-assisted backend generation.

The main value is simple: define or import Postgres tables, expose them as managed collections, add authentication, files, email, jobs, backups, logs, API keys, and AI/MCP automation from one backend.

## Why It Helps SaaS Builders

Most SaaS products repeat the same backend work:

- User signup, login, verification, password reset, email change, OTP, and sessions
- Organizations, teams, roles, invitations, and member access
- CRUD tables for domain data
- API keys for integrations
- Admin-only dashboards
- Files and attachments
- SMTP email delivery
- Cron jobs for renewals, reminders, cleanup, and reports
- Database backups
- Audit logs and request logs
- Quotas, abuse controls, and operational metrics

Dublyobase gives these as backend primitives around Postgres instead of forcing every project to rebuild them from scratch. A developer can start with a clean project or import an existing Postgres schema, then use the admin panel and APIs to shape the backend.

## Why It Helps Ecommerce Builders

Ecommerce backends need structured data, relations, searchable records, media, secure admin access, and background automation. Dublyobase fits this well because every ecommerce object can be represented as a real Postgres table and exposed through collections.

Typical ecommerce collections:

- `products`
- `product_variants`
- `categories`
- `brands`
- `customers`
- `carts`
- `cart_items`
- `orders`
- `order_items`
- `addresses`
- `coupons`
- `inventory_movements`
- `reviews`
- `payments`
- `shipments`
- `returns`
- `media`

Dublyobase can manage these as collections, apply relations between them, expose CRUD APIs, store product images/files, protect customer-owned records with rules, and run scheduled jobs for cart cleanup, inventory checks, subscription renewal, and backup.

Dublyobase does not replace payment processors like Stripe, Paddle, Tap, or PayPal. Instead, it stores and manages the ecommerce data around them: payment customers, checkout sessions, webhooks, orders, invoices, subscriptions, licenses, and audit history.

## Core Capabilities for SaaS and Ecommerce

### 1. Postgres as the Product Database

Dublyobase manages real Postgres tables. This is important for serious SaaS and ecommerce projects because data remains portable, queryable, and inspectable outside the app.

You can:

- Create new collections from the admin UI
- Import existing Postgres tables
- Discover existing schemas
- Preview tables before importing them
- Use native Postgres relations and indexes
- Keep project data in project schemas
- Use standard SQL tooling when needed

This makes Dublyobase different from backends that hide data behind proprietary storage.

### 2. Collections and Fields

Collections are the app-facing model layer over Postgres tables. They are used to define fields, relations, validation, rules, and API behavior.

Useful field types for SaaS/ecommerce:

- Text for names, slugs, SKU codes, titles, notes
- Rich editor for product descriptions and blog content
- Number for prices, stock, weights, quantities
- Bool for active/published/default flags
- Email for customer and invitation flows
- URL for external resources
- Datetime/autodate for lifecycle events
- File for product media, invoices, avatars, attachments
- Relation for linking users, orgs, products, orders, variants, and categories
- Select for statuses, roles, order states, payment states
- JSON for provider payloads, metadata, webhook bodies, and flexible settings

### 3. Relations for Real Product Models

Complex apps are mostly relations. Dublyobase supports relation fields so the admin can build models such as:

- One organization has many members
- One customer has many orders
- One product has many variants
- One order has many order items
- One product belongs to many categories through a join collection
- One user has one profile
- One subscription belongs to one organization

Example ecommerce relation layout:

```text
products
  id
  title
  slug
  description
  status
  default_image

product_variants
  id
  product -> products
  sku
  price
  inventory_quantity

orders
  id
  customer -> users
  organization -> organizations
  status
  total

order_items
  id
  order -> orders
  product -> products
  variant -> product_variants
  quantity
  unit_price
```

This is the base structure needed for storefronts, admin panels, customer portals, and order APIs.

### 4. App User Auth

Dublyobase includes app-user authentication for customer-facing apps.

Supported flows:

- Signup
- Login
- Refresh token
- Logout
- Logout all sessions
- Current user endpoint
- Email verification
- Password reset
- Email change with current password check
- Email OTP login
- Session/device listing
- Session revocation

For SaaS, this powers customer accounts, team dashboards, billing portals, and admin/member views.

For ecommerce, this powers customer accounts, order history, saved addresses, wishlists, support tickets, and review ownership.

### 5. Organizations, Teams, Roles, and Invitations

Complex SaaS often needs multi-tenant primitives. Dublyobase includes app-level organization support:

- Create organizations
- List organizations for current user
- Owner/admin/member/viewer-style roles
- Invite users by email
- Accept invitation tokens
- List organization members

This is useful for:

- B2B SaaS workspaces
- Agency/client accounts
- Team billing
- Multi-store ecommerce
- Vendor portals
- Marketplaces
- Franchise/location management

Example SaaS entities:

```text
organizations
organization_members
organization_invitations
projects
subscriptions
api_keys
usage_events
```

### 6. Rules, API Keys, and Access Control

Dublyobase gives every project API access paths for records and files. It supports app-user tokens and service API keys.

Useful patterns:

- Public catalog read access
- Authenticated customer order access
- Organization-scoped records
- Service-key admin jobs
- Internal integration access
- Read-only public APIs

Example ecommerce access rules:

- Products: public list/view when `status = "published"`
- Orders: customer can view only their own orders
- Order items: visible through owned orders
- Reviews: public list, authenticated create
- Inventory: admin/service only

### 7. Files and Product Media

Dublyobase supports file fields and upload APIs for:

- Product images
- Product galleries
- Avatars
- Digital downloads
- Invoices
- Attachments
- Brand assets
- Organization logos

Storage can be local or S3-compatible, such as Cloudflare R2, Backblaze B2, MinIO, and other compatible providers. This lets an ecommerce project store product media outside Postgres while keeping metadata attached to records.

### 8. SMTP and Auth Emails

SaaS and ecommerce need reliable transactional email. Dublyobase supports runtime SMTP settings and auth email templates.

Common emails:

- Verify account
- Reset password
- Email change confirmation
- OTP login code
- Organization invitation

For ecommerce, this can support account emails now. Order confirmation, shipping, and refund emails can be implemented through cron jobs, service integrations, or app-side email calls.

### 9. Cron Jobs

Dublyobase includes native HTTP cron jobs. This is useful for scheduled backend automation.

SaaS examples:

- Daily usage aggregation
- Subscription renewal checks
- Trial expiration reminders
- Cleanup expired invitations
- Sync external provider data
- Send weekly reports

Ecommerce examples:

- Expire abandoned carts
- Recalculate inventory alerts
- Sync payment provider status
- Send review request emails
- Update shipment states
- Generate daily sales reports
- Run scheduled backups

### 10. Backups

Dublyobase can create Postgres backup jobs and store backup files in configured storage.

This helps with:

- Nightly database backups
- Pre-deployment backup jobs
- Project-level data protection
- S3/R2/B2 backup storage
- Admin-visible backup runs

For production ecommerce, this is critical because orders, customers, and payment records must be recoverable.

### 11. Logs, Audit, and Metrics

Dublyobase includes:

- Admin audit logs
- Request logs
- Log pagination
- Project metrics
- Quota settings
- Request limits
- Auth request limits
- App user limits
- Storage quota enforcement

This helps operators answer:

- Who changed settings?
- Which API calls are failing?
- Which project is using the most requests?
- Are auth endpoints being abused?
- Is storage usage over budget?

### 12. MCP for AI Backend Automation

Dublyobase includes scoped MCP access so AI tools can work with the backend through explicit tools instead of direct database superuser access.

AI can be used to:

- Create collections
- Add fields
- Create relation models
- Upload files
- Create users
- Update SMTP/storage settings
- Generate backend structures from product requirements
- Build CRUD APIs for a SaaS or ecommerce app

This is useful for fast prototyping and for teams that want AI to build or modify backend structures with auditability and scoped tokens.

## Example: Building an Ecommerce Backend

### Step 1: Create Project

Create a project such as:

```text
slug: store
name: Store Backend
```

### Step 2: Create Core Collections

Start with:

```text
products
categories
product_variants
customers or users
carts
cart_items
orders
order_items
addresses
payments
shipments
coupons
reviews
```

### Step 3: Add Relations

Examples:

```text
product_variants.product -> products
cart_items.cart -> carts
cart_items.product -> products
cart_items.variant -> product_variants
orders.customer -> users
order_items.order -> orders
order_items.product -> products
reviews.product -> products
reviews.customer -> users
```

### Step 4: Configure Auth

Enable:

- Signup/login
- Email verification
- Password reset
- OTP if needed
- Session management

Use customers as app users. If the store is B2B, create organizations and invite team members.

### Step 5: Configure Storage

Use S3-compatible storage for:

- Product images
- Product galleries
- Digital files
- Invoices

For production, prefer R2/B2/MinIO/S3 over container-local storage.

### Step 6: Configure API Access

Suggested API behavior:

- Products: public read
- Categories: public read
- Carts: authenticated user only
- Orders: authenticated owner only
- Payments: service/admin only
- Inventory: service/admin only
- Reviews: public read, authenticated create

### Step 7: Add Cron Jobs

Useful jobs:

- Abandoned cart cleanup
- Inventory low-stock alert
- Payment status sync
- Daily sales report
- Nightly backup

### Step 8: Connect Storefront

A Next.js, Nuxt, Astro, mobile app, or custom frontend can call Dublyobase APIs for:

- Product listing
- Product detail
- Cart actions
- Checkout preparation
- Customer login
- Order history
- Profile updates
- File/download links

Payment confirmation should still be verified server-side through your payment provider webhooks.

## Example: Building a Multi-Tenant SaaS Backend

### Core Collections

```text
organizations
organization_members
projects
subscriptions
plans
usage_events
invoices
api_keys
audit_events
support_tickets
```

### Common Flow

1. User signs up.
2. User creates an organization.
3. Owner invites team members.
4. Organization creates project/workspace data.
5. App uses rules to scope data to the organization.
6. Cron aggregates usage.
7. Quotas prevent abuse.
8. Backups protect the database.
9. Logs help support and debugging.

## What Dublyobase Is Ready For Now

Dublyobase is ready for serious beta usage as a backend for:

- Internal SaaS tools
- Admin panels
- Customer portals
- Ecommerce prototypes
- B2B SaaS MVPs
- Marketplace MVPs
- Multi-tenant app experiments
- Existing Postgres admin/API layer
- AI-assisted backend generation

It is especially useful when the app needs real Postgres from day one and the developer wants to avoid building auth, admin UI, files, logs, SMTP, cron, backups, and API scaffolding manually.

## Current Production Notes

For production deployments, use conservative defaults:

- Run one app replica until realtime/webhook fanout is fully multi-replica hardened.
- Use managed Postgres with automated provider backups.
- Configure Dublyobase backup jobs to S3-compatible storage.
- Rotate the bootstrap admin password immediately.
- Keep `DATABASE_URL`, SMTP credentials, storage keys, and MCP tokens private.
- Use HTTPS and a correct `APP_URL`.
- Lock CORS to known domains.
- Use service API keys only from server-side code.
- Prefer S3-compatible object storage for files.

## Current Gaps to Track

Dublyobase is useful now, but these features should be completed before calling it a mature production Supabase alternative:

- OAuth provider runtime
- MFA beyond email OTP
- Durable multi-replica realtime/webhook delivery
- More complete restore UX with dry-run and safety warnings
- Typed SDK generation
- Schema migration/versioning workflows for large live apps
- Per-user, per-organization, and per-API-key abuse controls
- More automated browser tests against real SaaS/ecommerce demo frontends
- Official admin recovery command so password resets are auditable without manual SQL

## Recommended Positioning

Use this positioning:

```text
Dublyobase is an open-source Postgres backend platform for developers who want PocketBase-like speed with real PostgreSQL. It manages database collections, auth, teams, files, email, logs, cron jobs, backups, and AI/MCP automation from one admin panel.
```

For ecommerce:

```text
Dublyobase helps developers build ecommerce backends faster by turning Postgres tables for products, variants, carts, orders, customers, payments, media, coupons, and reviews into managed collections with APIs, auth, file storage, logs, and scheduled jobs.
```

For SaaS:

```text
Dublyobase helps developers build multi-tenant SaaS backends faster with app-user auth, organizations, roles, invitations, API keys, quotas, records CRUD, storage, SMTP, cron jobs, backups, logs, and Postgres schema management.
```
