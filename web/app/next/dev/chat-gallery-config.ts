// The hosts the link cards recognise are the page's config; the gallery has none, so this module (imported first) fills
// in example ones on the object the page already holds. Development only.
import { appConfig } from '@/platform/config';

Object.assign(appConfig(), {
  artifactsUrl: 'https://docs.example.com',
  boardsUrl: 'https://boards.example.com',
  standDomain: 'preview.example.com',
  trackerIssueUrl: 'https://tracker.example.com/issue/{key}',
  trackerHosts: ['tracker.example.com'],
  gitlabHosts: ['gitlab.example.com'],
});
