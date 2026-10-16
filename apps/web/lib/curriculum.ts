export type CurriculumProfile = {
  edition: string;
  edition_locked: boolean;
  volumes: string[];
  note: string;
};
export type ExamProfile = {
  region: string;
  policy_year: number;
  minutes: number;
  points: number;
  paper: string;
  sections: { name: string; kinds: string[]; note: string }[];
  note: string;
  sources: string[];
};
export type CatalogSubject = {
  id: string;
  types: { id: string; name: string; objective: boolean }[];
  curriculum?: CurriculumProfile;
  exam?: ExamProfile;
  published_counts?: Record<string, number>;
};
export type Recognition = {
  subject_id: string;
  edition: string;
  volume: string;
  source_kind: string;
  confidence: string;
  evidence: { page: number; quote: string }[];
  notes: string;
  edition_conflict?: boolean;
};
