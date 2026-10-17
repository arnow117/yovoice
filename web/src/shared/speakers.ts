import type { Draft } from './workbench';

export interface SpeakerRuntime {
  configured: boolean;
  threshold?: number;
  margin?: number;
  timeoutSeconds?: number;
  modelSha256?: string;
  featureVersion?: string;
  dimension?: number;
}

export interface SpeakerProfile {
  id: string;
  name: string;
  createdAt: string;
  bindVoiceId?: string;
  modelSha256: string;
  featureVersion: string;
  dimension: number;
}

export interface SpeakerCandidate {
  profileId: string;
  name: string;
  score: number;
}

export interface SpeakerIdentification {
  status: 'matched' | 'unknown' | 'ambiguous';
  reason: string;
  profileId?: string;
  score?: number;
  candidates: SpeakerCandidate[];
  threshold: number;
  margin: number;
  modelSha256: string;
  featureVersion: string;
}

export type SpeakerSource = { base64: string } | { voiceId: string };

export interface SpeakerEnrollment {
  requestId: string;
  source: SpeakerSource;
  name?: string;
  bindVoiceId?: string;
}

export interface SpeakerIdentificationRequest {
  requestId: string;
  source: SpeakerSource;
}

export interface SpeakerGenerationRequest {
  requestId: string;
  profileId: string;
  draft: Draft;
}
