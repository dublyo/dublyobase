import type { Collection, CollectionIconOption, CollectionOptions, Field } from "../../src/lib/types";
import type { navItems, settingsItems } from "./constants";

// View-layer types shared across the panel, split out of page.tsx.

export type View = (typeof navItems)[number]["id"];

export type SettingsSection = (typeof settingsItems)[number]["id"];

export type Notice = { type: "success" | "error"; message: string } | null;

export type CollectionModalMode = "create" | "settings" | null;

export type CollectionsMode = "records" | "overview";

export type OverviewTab = "fields" | "rules";

export type InsightsTab = "overview" | "collections" | "dashboards" | "ops";

export type InsightsRangeHours = 1 | 24 | 168 | 720;

export type RelationCardinality = "many_to_one" | "one_to_one" | "one_to_many" | "many_to_many";

export type RelationEdge = {
  sourceCollection: string;
  sourceField: string;
  targetCollection: string;
  displayField: string;
  cardinality: RelationCardinality;
  required: boolean;
  multiple: boolean;
};

export type CollectionDraft = {
  name: string;
  type: Collection["type"];
  icon: CollectionIconOption;
  fields: Field[];
  listRule: string;
  viewRule: string;
  createRule: string;
  updateRule: string;
  deleteRule: string;
};

export type RuleDraft = Pick<CollectionDraft, "listRule" | "viewRule" | "createRule" | "updateRule" | "deleteRule">;

export type RelationAnchor = {
  collection: string;
  value: string;
  sourceField: string;
  // set only for explicit anchors: the field ON THIS FORM that supplied the
  // value. Derived anchors leave it unset, because their sourceField names a
  // column on another collection and may coincidentally match a field here —
  // `payments.patient` and `invoices.patient` share a name but are not the
  // same field, and treating them as one silently disabled the scope.
  formField?: string;
  viaCollection?: string;
};

export type RelationConstraint = {
  filter: string;
  reasons: string[];
};

// Turn the anchors into a filter for one picker. Two shapes:
//
//   identity  an anchor IS a record of the collection being picked, so the
//             answer is already determined — offer only that record
//   link      the collection being picked has exactly one relation to an
//             anchor's collection, so filter by it
//
// A self-referencing field (an appointment's recurrence parent) also excludes
// the record being edited, which no schema fact can express.
