"use client";

import type { LucideIcon } from "lucide-react";

import { health } from "../../src/lib/api";
import type { AuditEntry, Collection, CollectionInsights, Field, InsightNameValue, Project, ProjectInsights, RequestLogEntry } from "../../src/lib/types";
import { insightRanges } from "../lib/constants";
import { buildLogBuckets, formatBytes, formatCompactNumber, formatCount, formatDurationMs, formatInsightTick, formatPercent } from "../lib/format";
import type { InsightsRangeHours, InsightsTab } from "../lib/view-types";
import { EmptyState, Info, PageFooter } from "./ui";
import type { ApexOptions } from "apexcharts";
import { Activity, AlertCircle, BriefcaseBusiness, Database, HardDrive, Layers3, ListFilter, Plus, RefreshCw, Server, ShieldCheck, ShoppingCart, Table2, Type, User, Users } from "lucide-react";
import dynamic from "next/dynamic";

export const ApexChart = dynamic(() => import("react-apexcharts"), { ssr: false });

export function InsightsWorkspace({
  project,
  collections,
  selectedCollection,
  selectedCollectionName,
  projectInsights,
  collectionInsights,
  tab,
  setTab,
  range,
  setRange,
  loading,
  onSelectCollection,
  onRefresh,
  version,
}: {
  project: Project | null;
  collections: Collection[];
  selectedCollection: Collection | null;
  selectedCollectionName: string;
  projectInsights: ProjectInsights | null;
  collectionInsights: CollectionInsights | null;
  tab: InsightsTab;
  setTab: (tab: InsightsTab) => void;
  range: InsightsRangeHours;
  setRange: (range: InsightsRangeHours) => void;
  loading: boolean;
  onSelectCollection: (name: string) => void;
  onRefresh: () => void;
  version: string;
}) {
  const analyticsCollections = collections.filter((collection) => collection.type !== "view");
  const metrics = projectInsights?.metrics;
  const tabItems: Array<{ id: InsightsTab; label: string; icon: LucideIcon }> = [
    { id: "overview", label: "Overview", icon: Activity },
    { id: "collections", label: "Collections", icon: Table2 },
    { id: "dashboards", label: "Dashboards", icon: Layers3 },
    { id: "ops", label: "Ops", icon: Server },
  ];
  const requestLabels = projectInsights?.requests.map((bucket) => formatInsightTick(bucket.timestamp, projectInsights.range.hours)) ?? [];
  const requestSeries = [
    { name: "Requests", data: projectInsights?.requests.map((bucket) => bucket.total) ?? [] },
    { name: "Errors", data: projectInsights?.requests.map((bucket) => bucket.errors) ?? [] },
  ];
  const latencySeries = [
    { name: "Avg", data: projectInsights?.requests.map((bucket) => bucket.avgDurationMs) ?? [] },
    { name: "P95", data: projectInsights?.requests.map((bucket) => bucket.p95DurationMs) ?? [] },
  ];
  const collectionLabels = collectionInsights?.created.map((bucket) => formatInsightTick(bucket.timestamp, collectionInsights.range.hours)) ?? [];
  const collectionSeries = [{ name: "New records", data: collectionInsights?.created.map((bucket) => bucket.count) ?? [] }];
  const insightCollectionRows = projectInsights?.collections ?? [];
  const collectionRows =
    insightCollectionRows.length > 0
      ? insightCollectionRows
      : analyticsCollections.map((collection) => ({
          name: collection.name,
          type: collection.type,
          system: collection.system,
          records: 0,
          newRecords: 0,
          fields: collection.fields.length,
        }));
  const topRecordCollections = collectionRows.slice().sort((a, b) => b.records - a.records).slice(0, 8);
  const topGrowthCollections = collectionRows.slice().sort((a, b) => b.newRecords - a.newRecords).slice(0, 8);
  const selectedSummary = collectionRows.find((collection) => collection.name === selectedCollectionName);
  const totalRecords = collectionRows.reduce((sum, collection) => sum + collection.records, 0);
  const totalNewRecords = collectionRows.reduce((sum, collection) => sum + collection.newRecords, 0);
  const errorRate = metrics?.requests.total ? (metrics.requests.errors / metrics.requests.total) * 100 : 0;
  const p95 = metrics?.requests.p95DurationMs ?? 0;
  const storageLimitBytes = (metrics?.quota.maxStorageMb ?? 0) * 1024 * 1024;
  const storageUsage = storageLimitBytes > 0 ? Math.min(100, ((metrics?.storageBytes ?? 0) / storageLimitBytes) * 100) : 0;
  const userUsage = metrics?.quota.maxAppUsers ? Math.min(100, ((metrics?.appUsers ?? 0) / metrics.quota.maxAppUsers) * 100) : 0;
  const opsItems = [
    {
      label: "Request health",
      value: `${formatPercent(errorRate)} errors`,
      tone: errorRate >= 10 ? "danger" : errorRate > 0 ? "warning" : "success",
      detail: `${formatCount(metrics?.requests.total ?? 0)} requests in range`,
    },
    {
      label: "Latency",
      value: formatDurationMs(p95),
      tone: p95 >= 1500 ? "danger" : p95 >= 500 ? "warning" : "success",
      detail: "P95 response time",
    },
    {
      label: "Storage quota",
      value: storageLimitBytes > 0 ? `${formatPercent(storageUsage)} used` : "Not capped",
      tone: storageUsage >= 90 ? "danger" : storageUsage >= 70 ? "warning" : "success",
      detail: storageLimitBytes > 0 ? `${formatBytes(metrics?.storageBytes ?? 0)} of ${formatBytes(storageLimitBytes)}` : formatBytes(metrics?.storageBytes ?? 0),
    },
    {
      label: "User quota",
      value: metrics?.quota.maxAppUsers ? `${formatPercent(userUsage)} used` : "Not capped",
      tone: userUsage >= 90 ? "danger" : userUsage >= 70 ? "warning" : "success",
      detail: `${formatCount(metrics?.appUsers ?? 0)} app users`,
    },
  ] as const;
  const dashboardPresets = [
    {
      title: "Marketplace catalog",
      icon: ShoppingCart,
      detail: "Products, tags, categories, media growth, and public API traffic.",
      metric: `${formatCount(totalRecords)} records`,
      panels: ["Collection ranking", "New catalog items", "Top API paths", "Field coverage"],
    },
    {
      title: "SaaS operations",
      icon: BriefcaseBusiness,
      detail: "Auth users, sessions, organizations, quotas, and latency.",
      metric: `${formatCount(metrics?.organizations ?? 0)} orgs`,
      panels: ["App users", "Active sessions", "Quota usage", "Error rate"],
    },
    {
      title: "API reliability",
      icon: ShieldCheck,
      detail: "Request volume, response classes, slow endpoints, and failure hotspots.",
      metric: `${formatPercent(errorRate)} errors`,
      panels: ["Request volume", "Latency", "Status classes", "Top paths"],
    },
  ];

  return (
    <section className="pb-page single">
      <div className="pb-page-content full-height pb-insights-module">
      <header className="pb-module-header">
        <div>
          <p className="pb-kicker">Insights</p>
          <h1>{project ? project.name : "No project"}</h1>
        </div>
        <div className="pb-header-actions">
          <div className="pb-segmented-control compact" role="radiogroup" aria-label="Insights range">
            {insightRanges.map((item) => (
              <button key={item.hours} type="button" role="radio" aria-checked={range === item.hours} className={range === item.hours ? "active" : ""} onClick={() => setRange(item.hours)}>
                {item.label}
              </button>
            ))}
          </div>
          <button type="button" className="pb-btn secondary" onClick={onRefresh} disabled={!project || loading}>
            <RefreshCw className={`h-4 w-4 ${loading ? "animate-spin" : ""}`} />
            Refresh
          </button>
        </div>
      </header>

      <div className="pb-insights-tabs" role="tablist" aria-label="Insights">
        {tabItems.map((item) => {
          const Icon = item.icon;
          return (
            <button key={item.id} type="button" role="tab" aria-selected={tab === item.id} className={tab === item.id ? "active" : ""} onClick={() => setTab(item.id)}>
              <Icon className="h-4 w-4" />
              {item.label}
            </button>
          );
        })}
      </div>

      {!project ? (
        <EmptyState title="Choose a project" detail="Insights are scoped to the selected project." />
      ) : tab === "overview" ? (
        <div className="pb-insights-scroll">
          <div className="pb-insights-stack">
            <section className="pb-insights-hero">
              <div>
                <h2>Project command center</h2>
                <p>
                  {formatCount(collectionRows.length)} collections, {formatCount(totalRecords)} records, {formatCount(totalNewRecords)} new records in this range.
                </p>
              </div>
              <div className="pb-insights-health">
                <span className={`pb-health-dot ${errorRate > 0 ? "warning" : "success"}`} />
                <strong>{errorRate > 0 ? "Needs attention" : "Healthy"}</strong>
                <em>{formatPercent(errorRate)} error rate</em>
              </div>
            </section>
          <div className="pb-insight-kpis">
            <InsightKPI icon={Activity} label="Requests" value={formatCount(metrics?.requests.total ?? 0)} detail={`${formatPercent(errorRate)} errors`} />
            <InsightKPI icon={AlertCircle} label="Errors" value={formatCount(metrics?.requests.errors ?? 0)} tone={(metrics?.requests.errors ?? 0) > 0 ? "warning" : "success"} detail="4xx and 5xx responses" />
            <InsightKPI icon={Server} label="P95 latency" value={formatDurationMs(metrics?.requests.p95DurationMs ?? 0)} tone={(metrics?.requests.p95DurationMs ?? 0) > 1000 ? "warning" : "default"} detail={`Avg ${formatDurationMs(metrics?.requests.avgDurationMs ?? 0)}`} />
            <InsightKPI icon={Users} label="App users" value={formatCount(metrics?.appUsers ?? 0)} detail={`${formatCount(metrics?.activeSessions ?? 0)} sessions`} />
            <InsightKPI icon={BriefcaseBusiness} label="Organizations" value={formatCount(metrics?.organizations ?? 0)} detail="SaaS workspaces" />
            <InsightKPI icon={HardDrive} label="Storage" value={formatBytes(metrics?.storageBytes ?? 0)} detail={storageLimitBytes > 0 ? `${formatPercent(storageUsage)} of quota` : "No quota cap"} />
          </div>
          <div className="pb-insights-grid two">
            <InsightChart title="Request volume" detail="Traffic and errors over time" type="area" labels={requestLabels} series={requestSeries} empty={!projectInsights || projectInsights.requests.every((bucket) => bucket.total === 0)} />
            <InsightChart title="Latency" detail="Average and P95 response time" type="line" labels={requestLabels} series={latencySeries} empty={!projectInsights || projectInsights.requests.every((bucket) => bucket.p95DurationMs === 0)} colors={["#0f766e", "#7c3aed"]} />
          </div>
          <div className="pb-insights-grid three">
            <InsightNameValuePanel title="Methods" items={projectInsights?.methods ?? []} />
            <InsightNameValuePanel title="Status classes" items={projectInsights?.statuses ?? []} />
            <InsightNameValuePanel title="Top paths" items={projectInsights?.topPaths ?? []} wide />
          </div>
          <div className="pb-insights-grid two">
            <InsightChart title="Largest collections" detail="Top record counts" type="bar" labels={topRecordCollections.map((item) => item.name)} series={[{ name: "Records", data: topRecordCollections.map((item) => item.records) }]} empty={topRecordCollections.length === 0} colors={["#2563eb"]} height={320} />
            <InsightChart title="Fastest growing collections" detail="New records in selected range" type="bar" labels={topGrowthCollections.map((item) => item.name)} series={[{ name: "New records", data: topGrowthCollections.map((item) => item.newRecords) }]} empty={topGrowthCollections.every((item) => item.newRecords === 0)} colors={["#16a34a"]} height={320} />
          </div>
          <section className="pb-settings-block">
            <div className="pb-section-title-row">
              <div>
                <h3>Collection activity</h3>
                <p className="pb-muted-copy">{formatCount(collectionRows.length)} collections in this project.</p>
              </div>
            </div>
            <div className="pb-table-wrap">
              <table className="pb-records-table compact">
                <thead>
                  <tr>
                    <th>Collection</th>
                    <th>Type</th>
                    <th>Records</th>
                    <th>New</th>
                    <th>Fields</th>
                  </tr>
                </thead>
                <tbody>
                  {collectionRows.map((collection) => (
                    <tr key={collection.name}>
                      <td>{collection.name}</td>
                      <td>{collection.type}</td>
                      <td>{formatCount(collection.records)}</td>
                      <td>{formatCount(collection.newRecords)}</td>
                      <td>{formatCount(collection.fields)}</td>
                    </tr>
                  ))}
                  {collectionRows.length === 0 ? (
                    <tr>
                      <td colSpan={5}>No collections found.</td>
                    </tr>
                  ) : null}
                </tbody>
              </table>
            </div>
          </section>
          </div>
        </div>
      ) : tab === "collections" ? (
        <div className="pb-insights-scroll">
          <div className="pb-insights-split">
            <aside className="pb-insights-sidebar">
              <div className="pb-section-title-row">
                <div>
                  <h3>Collections</h3>
                  <p className="pb-muted-copy">Pick a table to inspect field quality and growth.</p>
                </div>
              </div>
              <label className="pb-field">
                <span>Collection</span>
                <select value={selectedCollectionName} onChange={(event) => onSelectCollection(event.target.value)} disabled={analyticsCollections.length === 0}>
                  {analyticsCollections.map((collection) => (
                    <option key={collection.id} value={collection.name}>
                      {collection.name}
                    </option>
                  ))}
                </select>
              </label>
              <div className="pb-collection-insight-list" role="list">
                {analyticsCollections.map((collection) => {
                  const summary = collectionRows.find((item) => item.name === collection.name);
                  return (
                    <button key={collection.id} type="button" className={selectedCollectionName === collection.name ? "active" : ""} onClick={() => onSelectCollection(collection.name)}>
                      <span>
                        <strong>{collection.name}</strong>
                        <em>{formatCount(summary?.records ?? 0)} records</em>
                      </span>
                      <b>{formatCount(summary?.newRecords ?? 0)}</b>
                    </button>
                  );
                })}
              </div>
            </aside>
            <div className="pb-insights-stack">
          <div className="pb-insight-kpis">
            <InsightKPI icon={Database} label="Records" value={formatCount(collectionInsights?.records ?? selectedSummary?.records ?? 0)} detail={selectedCollectionName || "No collection"} />
            <InsightKPI icon={Plus} label="New records" value={formatCount(collectionInsights?.newRecords ?? selectedSummary?.newRecords ?? 0)} detail={insightRanges.find((item) => item.hours === range)?.label ?? "24h"} />
            <InsightKPI icon={ListFilter} label="Analyzed fields" value={formatCount(collectionInsights?.fields.length ?? 0)} detail={`${formatCount(selectedCollection?.fields.length ?? 0)} configured fields`} />
            <InsightKPI icon={Table2} label="Field count" value={formatCount(selectedSummary?.fields ?? selectedCollection?.fields.length ?? 0)} detail={selectedCollection?.type ?? "base"} />
          </div>
          <InsightChart title="Created records" detail={collectionInsights?.collection ?? selectedCollectionName} type="bar" labels={collectionLabels} series={collectionSeries} empty={!collectionInsights || collectionInsights.created.every((bucket) => bucket.count === 0)} height={320} />
          <section className="pb-settings-block">
            <div className="pb-section-title-row">
              <div>
                <h3>Field analysis</h3>
                <p className="pb-muted-copy">{collectionInsights?.collection ?? selectedCollectionName}</p>
              </div>
            </div>
            <div className="pb-insight-field-grid">
              {(collectionInsights?.fields ?? []).map((field) => (
                <FieldInsightCard key={field.name} field={field} total={collectionInsights?.records ?? 0} />
              ))}
              {collectionInsights?.fields.length === 0 ? <EmptyState title="No analyzable fields" detail="Hidden, file, JSON, editor, and password fields are skipped." /> : null}
            </div>
          </section>
            </div>
          </div>
        </div>
      ) : tab === "dashboards" ? (
        <div className="pb-insights-scroll">
          <div className="pb-insights-stack">
            <div className="pb-dashboard-grid">
              {dashboardPresets.map((preset) => (
                <DashboardPresetCard key={preset.title} preset={preset} />
              ))}
            </div>
            <div className="pb-insights-grid two">
              <InsightChart title="API dashboard" detail="Requests and failures" type="area" labels={requestLabels} series={requestSeries} empty={!projectInsights || projectInsights.requests.every((bucket) => bucket.total === 0)} />
              <InsightChart title="Data dashboard" detail="Collections by size" type="bar" labels={topRecordCollections.map((item) => item.name)} series={[{ name: "Records", data: topRecordCollections.map((item) => item.records) }]} empty={topRecordCollections.length === 0} colors={["#7c3aed"]} />
            </div>
          <section className="pb-settings-block">
            <div className="pb-section-title-row">
              <div>
                <h3>Saved dashboards</h3>
                <p className="pb-muted-copy">Preset panels are live now. Persisted custom dashboard layouts can build on the same panel model.</p>
              </div>
              <button type="button" className="pb-btn secondary" disabled>
                <Plus className="h-4 w-4" />
                New dashboard
              </button>
            </div>
            <div className="pb-dashboard-preview">
              <div>
                <strong>Current live panels</strong>
                <span>Request volume, latency, top paths, collection ranking, field analysis, and quota health.</span>
              </div>
              <div>
                <strong>Next custom panels</strong>
                <span>Saved filters, chart choice, layout persistence, and per-admin dashboard presets.</span>
              </div>
            </div>
          </section>
          </div>
        </div>
      ) : (
        <div className="pb-insights-scroll">
          <div className="pb-insights-stack">
            <div className="pb-insight-kpis">
              <InsightKPI icon={AlertCircle} label="Error rate" value={formatPercent(errorRate)} tone={errorRate > 0 ? "warning" : "success"} detail={`${formatCount(metrics?.requests.errors ?? 0)} failed requests`} />
              <InsightKPI icon={Server} label="P95 latency" value={formatDurationMs(p95)} tone={p95 >= 1500 ? "warning" : "default"} detail={`Avg ${formatDurationMs(metrics?.requests.avgDurationMs ?? 0)}`} />
              <InsightKPI icon={Users} label="Sessions" value={formatCount(metrics?.activeSessions ?? 0)} detail="Active app sessions" />
              <InsightKPI icon={HardDrive} label="Storage" value={formatBytes(metrics?.storageBytes ?? 0)} detail={storageLimitBytes > 0 ? `${formatBytes(storageLimitBytes)} quota` : "No storage cap"} />
            </div>
            <div className="pb-insights-grid two">
              <section className="pb-settings-block pb-ops-panel">
                <div className="pb-section-title-row">
                  <div>
                    <h3>Operations health</h3>
                    <p className="pb-muted-copy">Signals that matter before a backend becomes a production blocker.</p>
                  </div>
                </div>
                <div className="pb-ops-checks">
                  {opsItems.map((item) => (
                    <div key={item.label} className={`pb-ops-check ${item.tone}`}>
                      <span className={`pb-health-dot ${item.tone}`} />
                      <div>
                        <strong>{item.label}</strong>
                        <em>{item.detail}</em>
                      </div>
                      <b>{item.value}</b>
                    </div>
                  ))}
                </div>
              </section>
              <InsightChart title="Response classes" detail="HTTP status distribution" type="bar" labels={(projectInsights?.statuses ?? []).map((item) => item.name)} series={[{ name: "Responses", data: (projectInsights?.statuses ?? []).map((item) => item.value) }]} empty={!projectInsights || projectInsights.statuses.length === 0} colors={["#dc2626"]} />
            </div>
            <div className="pb-insights-grid two">
              <InsightNameValuePanel title="Slow/frequent surfaces" items={projectInsights?.topPaths ?? []} wide />
              <InsightNameValuePanel title="Methods" items={projectInsights?.methods ?? []} />
            </div>
          </div>
        </div>
      )}
      <PageFooter left="Insights" version={version} />
      </div>
    </section>
  );
}

export function InsightKPI({ label, value, detail, icon: Icon, tone = "default" }: { label: string; value: string; detail?: string; icon?: LucideIcon; tone?: "default" | "success" | "warning" }) {
  return (
    <div className={`pb-insight-kpi ${tone}`}>
      <span>
        {Icon ? <Icon className="h-4 w-4" /> : null}
        {label}
      </span>
      <strong>{value}</strong>
      {detail ? <em>{detail}</em> : null}
    </div>
  );
}

export function InsightChart({
  title,
  detail,
  labels,
  series,
  type,
  empty,
  colors,
  height = 280,
}: {
  title: string;
  detail?: string;
  labels: string[];
  series: Array<{ name: string; data: number[] }>;
  type: "line" | "area" | "bar";
  empty: boolean;
  colors?: string[];
  height?: number;
}) {
  const options: ApexOptions = {
    chart: {
      toolbar: { show: false },
      animations: { enabled: false },
      fontFamily: "inherit",
      foreColor: "var(--surface-hint)",
      zoom: { enabled: false },
    },
    colors: colors ?? ["#2563eb", "#dc2626", "#16a34a"],
    dataLabels: { enabled: false },
    fill: { opacity: type === "area" ? 0.24 : 1, type: type === "area" ? "gradient" : "solid", gradient: { opacityFrom: 0.36, opacityTo: 0.04 } },
    grid: { borderColor: "var(--surface-alt-2)", strokeDashArray: 3 },
    legend: { position: "top", horizontalAlign: "left", fontSize: "12px", markers: { size: 6 } },
    plotOptions: { bar: { borderRadius: 5, columnWidth: "54%", horizontal: labels.some((label) => label.length > 18) } },
    stroke: { curve: "smooth", width: type === "bar" ? 0 : 2 },
    xaxis: { categories: labels, labels: { rotate: -25, hideOverlappingLabels: true, trim: true } },
    yaxis: { labels: { formatter: (value) => formatCompactNumber(value) } },
    tooltip: { theme: "light", shared: true, intersect: false, y: { formatter: (value) => formatCount(Math.round(value)) } },
  };
  return (
    <section className="pb-settings-block pb-chart-card">
      <div className="pb-section-title-row">
        <div>
          <h3>{title}</h3>
          {detail ? <p className="pb-muted-copy">{detail}</p> : null}
        </div>
      </div>
      {empty ? <EmptyState title="No data" detail="No matching events in this range." compact /> : <ApexChart options={options} series={series} type={type} height={height} />}
    </section>
  );
}

export function InsightNameValuePanel({ title, items, wide = false }: { title: string; items: InsightNameValue[]; wide?: boolean }) {
  const max = Math.max(1, ...items.map((item) => item.value));
  return (
    <section className={`pb-settings-block pb-insight-list ${wide ? "wide" : ""}`}>
      <div className="pb-section-title-row">
        <h3>{title}</h3>
      </div>
      {items.length === 0 ? <EmptyState title="No data" detail="No matching events in this range." compact /> : null}
      {items.map((item) => (
        <div key={item.name} className="pb-insight-rank-row">
          <span title={item.name}>{item.name}</span>
          <div aria-hidden="true">
            <i style={{ width: `${Math.max(4, Math.round((item.value / max) * 100))}%` }} />
          </div>
          <strong>{formatCount(item.value)}</strong>
        </div>
      ))}
    </section>
  );
}

export function DashboardPresetCard({
  preset,
}: {
  preset: {
    title: string;
    icon: LucideIcon;
    detail: string;
    metric: string;
    panels: string[];
  };
}) {
  const Icon = preset.icon;
  return (
    <article className="pb-dashboard-card">
      <header>
        <span>
          <Icon className="h-5 w-5" />
        </span>
        <strong>{preset.metric}</strong>
      </header>
      <h3>{preset.title}</h3>
      <p>{preset.detail}</p>
      <div className="pb-chip-row">
        {preset.panels.map((panel) => (
          <span key={panel} className="pb-chip">
            {panel}
          </span>
        ))}
      </div>
    </article>
  );
}

export function FieldInsightCard({ field, total }: { field: CollectionInsights["fields"][number]; total: number }) {
  const fillRate = total > 0 ? Math.round((field.filled / total) * 100) : 0;
  return (
    <article className="pb-field-insight-card">
      <header>
        <span>
          <strong>{field.name}</strong>
          <em>{field.type}</em>
        </span>
        <b>{fillRate}%</b>
      </header>
      <div className="pb-fill-meter" aria-label={`${field.name} fill rate ${fillRate}%`}>
        <span style={{ width: `${fillRate}%` }} />
      </div>
      <div className="pb-info-grid compact">
        <Info label="Filled" value={formatCount(field.filled)} />
        <Info label="Empty" value={formatCount(field.empty)} />
        <Info label="Distinct" value={formatCount(field.distinct)} />
      </div>
      {field.numeric ? (
        <div className="pb-info-grid compact">
          <Info label="Sum" value={formatCompactNumber(field.numeric.sum)} />
          <Info label="Avg" value={formatCompactNumber(field.numeric.avg)} />
          <Info label="Min" value={formatCompactNumber(field.numeric.min)} />
          <Info label="Max" value={formatCompactNumber(field.numeric.max)} />
        </div>
      ) : null}
      {field.top?.length ? (
        <div className="pb-chip-row">
          {field.top.slice(0, 6).map((item) => (
            <span key={item.name} className="pb-chip" title={item.name}>
              {item.name} · {formatCount(item.value)}
            </span>
          ))}
        </div>
      ) : null}
    </article>
  );
}

export function LogActivityPanel({
  mode,
  audit,
  requests,
  total,
  visible,
  errors,
}: {
  mode: "audit" | "requests";
  audit: AuditEntry[];
  requests: RequestLogEntry[];
  total: number;
  visible: number;
  errors: number;
}) {
  const buckets = buildLogBuckets(mode === "audit" ? audit : requests, mode);
  const max = Math.max(1, ...buckets.map((bucket) => bucket.count));
  return (
    <section className="pb-log-activity" aria-label={`${mode} activity`}>
      <div className="pb-log-summary">
        <span>
          <strong>{formatCount(total)}</strong>
          total
        </span>
        <span>
          <strong>{formatCount(visible)}</strong>
          visible
        </span>
        <span>
          <strong>{formatCount(errors)}</strong>
          attention
        </span>
      </div>
      <div className="pb-log-chart" aria-hidden="true">
        {buckets.map((bucket, index) => (
          <span key={`${bucket.label}-${index}`} title={`${bucket.label}: ${bucket.count}`} style={{ height: `${Math.max(8, Math.round((bucket.count / max) * 72))}px` }} className={bucket.errors > 0 ? "danger" : ""} />
        ))}
      </div>
      <div className="pb-log-chart-labels" aria-hidden="true">
        <span>{buckets[0]?.label ?? ""}</span>
        <span>{buckets[Math.floor(buckets.length / 2)]?.label ?? ""}</span>
        <span>{buckets[buckets.length - 1]?.label ?? ""}</span>
      </div>
    </section>
  );
}
