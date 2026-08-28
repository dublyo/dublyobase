import type { InstanceSettings, Project, ProjectAuthSettings, ProjectQuotas } from "../../src/lib/types";
import { emptyCORSDraft, emptyLogDraft, emptyQuotaDraft, emptySMTPDraft, emptyStorageDraft } from "./constants";

// Mapping server settings onto the panel's form drafts, split out of page.tsx.

export function settingsToSMTPDraft(settings: InstanceSettings): typeof emptySMTPDraft {
  return {
    enabled: settings.smtp.enabled,
    host: settings.smtp.host,
    port: String(settings.smtp.port || 587),
    from: settings.smtp.from,
    username: settings.smtp.username,
    password: "",
    clearPassword: false,
    testTo: "",
  };
}

export function defaultAuthSettingsForProject(project: Project | null): ProjectAuthSettings {
  return {
    projectId: project?.id ?? "",
    projectSlug: project?.slug ?? "",
    accessTokenMinutes: 60,
    refreshTokenDays: 7,
    verifyTokenHours: 24,
    resetTokenHours: 1,
    otpEnabled: true,
    otpTokenMinutes: 10,
    mfaEnabled: false,
    mfaRequired: false,
    emailChangeEnabled: true,
    emailChangeRequiresPassword: true,
    templates: {
      verifySubject: "Verify your email for {APP_NAME}",
      verifyBody: "Verify your email for {APP_NAME}.\n\nOpen this link:\n{LINK}\n\nToken:\n{TOKEN}\n",
      resetSubject: "Reset your {APP_NAME} password",
      resetBody: "Reset your password for {APP_NAME}.\n\nOpen this link:\n{LINK}\n\nToken:\n{TOKEN}\n",
      otpSubject: "Your {APP_NAME} login code",
      otpBody: "Use this one-time login code for {APP_NAME}:\n\n{TOKEN}\n\nThis code expires soon.\n",
      emailChangeSubject: "Confirm your new email for {APP_NAME}",
      emailChangeBody: "Confirm the new email address for {APP_NAME}.\n\nNew email: {NEW_EMAIL}\n\nOpen this link:\n{LINK}\n\nToken:\n{TOKEN}\n",
      invitationSubject: "You are invited to {APP_NAME}",
      invitationBody: "You were invited to {APP_NAME}.\n\nOpen this link:\n{LINK}\n\nInvitation token:\n{TOKEN}\n",
    },
    providers: {},
  };
}

export function settingsToStorageDraft(settings: InstanceSettings): typeof emptyStorageDraft {
  return {
    type: settings.storage.type,
    endpoint: settings.storage.s3.endpoint,
    bucket: settings.storage.s3.bucket,
    region: settings.storage.s3.region || "us-east-1",
    accessKey: settings.storage.s3.accessKey,
    secretKey: "",
    clearSecretKey: false,
    prefix: settings.storage.s3.prefix,
    useSSL: settings.storage.s3.useSSL,
    forcePathStyle: settings.storage.s3.forcePathStyle,
  };
}

export function settingsToLogDraft(settings: InstanceSettings): typeof emptyLogDraft {
  return {
    retentionDays: String(settings.logs.retentionDays || 30),
    retentionCount: String(settings.logs.retentionCount || 100000),
  };
}

export function quotasToDraft(quotas: ProjectQuotas | null): typeof emptyQuotaDraft {
  if (!quotas) return emptyQuotaDraft;
  return {
    enabled: quotas.enabled,
    requestsPerMinute: String(quotas.requestsPerMinute || 0),
    authRequestsPerMinute: String(quotas.authRequestsPerMinute || 0),
    maxAppUsers: String(quotas.maxAppUsers || 0),
    maxStorageMb: String(quotas.maxStorageMb || 0),
  };
}

export function settingsToCORSDraft(settings: InstanceSettings | null, project: Project | null): typeof emptyCORSDraft {
  return {
    adminOrigins: (settings?.cors.adminOrigins ?? []).join("\n"),
    publicOrigins: (project?.cors?.publicOrigins ?? settings?.cors.adminOrigins ?? []).join("\n"),
    allowAdminWildcard: Boolean(settings?.cors.wildcard),
    allowPublicWildcard: Boolean(project?.cors?.wildcard),
  };
}

export function splitDraftList(value: string) {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const item of value.split(/[\n,]/)) {
    const next = item.trim();
    if (!next || seen.has(next)) continue;
    seen.add(next);
    out.push(next);
  }
  return out;
}
