/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import type { MigrationSettings } from '../types'
import { createSectionRegistry } from '../utils/section-registry'
import { MigrationPanel } from './migration-panel'

const MIGRATION_SECTIONS = [
  {
    id: 'database',
    titleKey: 'Database Migration',
    build: (_settings: MigrationSettings) => <MigrationPanel />,
  },
] as const

export type MigrationSectionId = (typeof MIGRATION_SECTIONS)[number]['id']

const migrationRegistry = createSectionRegistry<
  MigrationSectionId,
  MigrationSettings
>({
  sections: MIGRATION_SECTIONS,
  defaultSection: 'database',
  basePath: '/system-settings/migration',
  urlStyle: 'path',
})

export const MIGRATION_SECTION_IDS = migrationRegistry.sectionIds
export const MIGRATION_DEFAULT_SECTION = migrationRegistry.defaultSection
export const getMigrationSectionNavItems = migrationRegistry.getSectionNavItems
export const getMigrationSectionContent = migrationRegistry.getSectionContent
export const getMigrationSectionMeta = migrationRegistry.getSectionMeta