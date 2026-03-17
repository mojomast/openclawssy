"""
becomussy – Journal service.

Provides CRUD, search, and summarization for journal entries.
"""

from __future__ import annotations

import uuid
from datetime import datetime, timezone
from typing import Any

from fastapi import HTTPException, status
from sqlalchemy import func, or_, select
from sqlalchemy.ext.asyncio import AsyncSession

from app.models.journal import JournalEntry
from app.schemas.common import PaginatedResponse
from app.schemas.journal import (
	JournalEntryCreate,
	JournalEntryRead,
	JournalEntryUpdate,
	JournalSearchParams,
)
from app.services.audit import AuditService


class JournalService:
	"""Service layer for journal entry operations."""

	@staticmethod
	async def create(
		session: AsyncSession,
		data: JournalEntryCreate,
		actor: str,
	) -> JournalEntry:
		"""Create a new journal entry and log an audit event."""
		entry = JournalEntry(
			id=uuid.uuid4(),
			timestamp=datetime.now(timezone.utc),
			entry_type=data.entry_type,
			title=data.title,
			body_md=data.body_md,
			confidence_level=data.confidence_level,
			tags=data.tags,
			linked_memory_ids=data.linked_memory_ids,
			linked_project_ids=data.linked_project_ids,
			linked_identity_themes=data.linked_identity_themes,
			follow_up_candidates=data.follow_up_candidates,
			provenance_json=data.provenance or {},
			created_by=actor,
			updated_by=actor,
		)
		session.add(entry)
		await session.flush()

		await AuditService.log_event(
			session,
			event_type="journal_created",
			entity_type="journal_entry",
			entity_id=entry.id,
			actor=actor,
			actor_type="user",
			after_json=_entry_to_dict(entry),
		)

		return entry

	@staticmethod
	async def get(
		session: AsyncSession,
		journal_id: uuid.UUID,
	) -> JournalEntry:
		"""Retrieve a single journal entry or raise 404."""
		result = await session.execute(
			select(JournalEntry).where(JournalEntry.id == journal_id)
		)
		entry = result.scalar_one_or_none()
		if entry is None:
			raise HTTPException(
				status_code=status.HTTP_404_NOT_FOUND,
				detail=f"Journal entry {journal_id} not found",
			)
		return entry

	@staticmethod
	async def search(
		session: AsyncSession,
		params: JournalSearchParams,
	) -> PaginatedResponse[JournalEntryRead]:
		"""Search journal entries with filters, returning a paginated result."""
		query = select(JournalEntry)
		count_query = select(func.count()).select_from(JournalEntry)

		if params.keyword:
			keyword_filter = or_(
				JournalEntry.title.ilike(f"%{params.keyword}%"),
				JournalEntry.body_md.ilike(f"%{params.keyword}%"),
			)
			query = query.where(keyword_filter)
			count_query = count_query.where(keyword_filter)

		if params.entry_type:
			query = query.where(JournalEntry.entry_type == params.entry_type)
			count_query = count_query.where(JournalEntry.entry_type == params.entry_type)

		if params.date_from:
			query = query.where(JournalEntry.timestamp >= params.date_from)
			count_query = count_query.where(JournalEntry.timestamp >= params.date_from)
		if params.date_to:
			query = query.where(JournalEntry.timestamp <= params.date_to)
			count_query = count_query.where(JournalEntry.timestamp <= params.date_to)

		if params.linked_project_id:
			project_filter = JournalEntry.linked_project_ids.contains([params.linked_project_id])
			query = query.where(project_filter)
			count_query = count_query.where(project_filter)

		if params.linked_theme:
			theme_filter = JournalEntry.linked_identity_themes.contains([params.linked_theme])
			query = query.where(theme_filter)
			count_query = count_query.where(theme_filter)

		total_result = await session.execute(count_query)
		total = total_result.scalar_one()

		query = query.order_by(JournalEntry.timestamp.desc()).limit(params.limit).offset(params.offset)
		result = await session.execute(query)
		rows = result.scalars().all()

		return PaginatedResponse[JournalEntryRead](
			items=[JournalEntryRead.model_validate(r) for r in rows],
			total=total,
			limit=params.limit,
			offset=params.offset,
		)

	@staticmethod
	async def update(
		session: AsyncSession,
		journal_id: uuid.UUID,
		data: JournalEntryUpdate,
		actor: str,
	) -> JournalEntry:
		"""Update a journal entry and log an audit event."""
		entry = await JournalService.get(session, journal_id)
		before = _entry_to_dict(entry)

		update_data = data.model_dump(exclude_unset=True)
		if "provenance" in update_data:
			update_data["provenance_json"] = update_data.pop("provenance")

		for field_name, value in update_data.items():
			setattr(entry, field_name, value)

		entry.updated_by = actor
		await session.flush()

		await AuditService.log_event(
			session,
			event_type="journal_updated",
			entity_type="journal_entry",
			entity_id=entry.id,
			actor=actor,
			actor_type="user",
			before_json=before,
			after_json=_entry_to_dict(entry),
		)

		return await JournalService.get(session, entry.id)

	@staticmethod
	async def summarize(
		session: AsyncSession,
		range_start: datetime,
		range_end: datetime,
		summary_type: str,
	) -> list[JournalEntryRead]:
		"""Return journal entries in the given range."""
		query = (
			select(JournalEntry)
			.where(JournalEntry.timestamp >= range_start)
			.where(JournalEntry.timestamp <= range_end)
			.order_by(JournalEntry.timestamp.asc())
		)
		result = await session.execute(query)
		rows = result.scalars().all()
		return [JournalEntryRead.model_validate(r) for r in rows]


def _entry_to_dict(entry: JournalEntry) -> dict[str, Any]:
	"""Serialize a JournalEntry to a plain dict for audit logging."""
	return {
		"id": str(entry.id),
		"timestamp": entry.timestamp.isoformat() if entry.timestamp else None,
		"entry_type": entry.entry_type,
		"title": entry.title,
		"body_md": entry.body_md,
		"confidence_level": entry.confidence_level,
		"tags": entry.tags,
		"linked_memory_ids": [str(uid) for uid in (entry.linked_memory_ids or [])],
		"linked_project_ids": [str(uid) for uid in (entry.linked_project_ids or [])],
		"linked_identity_themes": entry.linked_identity_themes,
		"follow_up_candidates": entry.follow_up_candidates,
		"provenance_json": entry.provenance_json,
		"created_by": entry.created_by,
		"updated_by": entry.updated_by,
	}
