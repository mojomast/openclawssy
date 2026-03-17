"""
becomussy – Memory service.

Provides CRUD, search, reinforce, and contradict operations for memory items.
"""

from __future__ import annotations

import uuid
from datetime import datetime, timezone
from decimal import Decimal

from fastapi import HTTPException, status
from sqlalchemy import func, select
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy.orm import selectinload

from app.core.security import CurrentUser
from app.models.memory import MemoryItem, MemoryLink
from app.schemas.memory import (
	MemoryItemCreate,
	MemoryItemUpdate,
	MemoryLinkCreate,
	MemorySearchParams,
)
from app.services.audit import AuditService


def _memory_to_dict(m: MemoryItem) -> dict:
	"""Convert a MemoryItem to a JSON-serialisable dict for audit logging."""
	return {
		"id": str(m.id),
		"memory_type": m.memory_type,
		"timestamp": m.timestamp.isoformat() if m.timestamp else None,
		"summary": m.summary,
		"statement": m.statement,
		"importance_score": float(m.importance_score) if m.importance_score is not None else None,
		"salience_score": float(m.salience_score) if m.salience_score is not None else None,
		"confidence_level": m.confidence_level,
		"status": m.status,
		"approval_state": m.approval_state,
		"source_kind": m.source_kind,
		"source_ref": m.source_ref,
	}


class MemoryService:
	"""Service layer for memory operations."""

	@staticmethod
	async def create(
		session: AsyncSession,
		data: MemoryItemCreate,
		actor: CurrentUser,
	) -> MemoryItem:
		"""Create a new memory item and log an audit event."""
		provenance = {}
		if data.provenance:
			provenance = data.provenance.model_dump(exclude_none=True)

		item = MemoryItem(
			id=uuid.uuid4(),
			memory_type=data.memory_type.value,
			timestamp=data.timestamp or datetime.now(timezone.utc),
			summary=data.summary,
			statement=data.statement,
			importance_score=data.importance_score,
			salience_score=Decimal("0.00"),
			confidence_level=data.confidence_level.value if data.confidence_level else None,
			status="active",
			approval_state="not_required",
			source_kind=data.source_kind or (data.provenance.source_kind if data.provenance else None),
			source_ref=data.source_ref or (data.provenance.source_ref if data.provenance else None),
			provenance_json=provenance,
			metadata_json=data.metadata or {},
			created_by=actor.user_id,
			updated_by=actor.user_id,
		)
		session.add(item)
		await session.flush()

		await AuditService.log_event(
			session,
			event_type="memory_created",
			entity_type="memory_item",
			entity_id=item.id,
			actor=actor.user_id,
			actor_type=actor.role.value,
			after_json=_memory_to_dict(item),
		)

		await session.refresh(item, attribute_names=["outgoing_links", "incoming_links"])

		return item

	@staticmethod
	async def get(session: AsyncSession, memory_id: uuid.UUID) -> MemoryItem:
		"""Retrieve a memory item by ID, or raise 404."""
		result = await session.execute(
			select(MemoryItem)
			.options(
				selectinload(MemoryItem.outgoing_links),
				selectinload(MemoryItem.incoming_links),
			)
			.where(MemoryItem.id == memory_id)
		)
		item = result.scalar_one_or_none()
		if item is None:
			raise HTTPException(
				status_code=status.HTTP_404_NOT_FOUND,
				detail=f"Memory item {memory_id} not found",
			)
		return item

	@staticmethod
	async def search(
		session: AsyncSession,
		params: MemorySearchParams,
	) -> tuple[list[MemoryItem], int]:
		"""Search/filter memory items with pagination. Returns (items, total)."""
		query = select(MemoryItem).options(
			selectinload(MemoryItem.outgoing_links),
			selectinload(MemoryItem.incoming_links),
		)
		count_query = select(func.count()).select_from(MemoryItem)

		if params.q:
			like_pattern = f"%{params.q}%"
			filter_cond = (
				MemoryItem.summary.ilike(like_pattern)
				| MemoryItem.statement.ilike(like_pattern)
			)
			query = query.where(filter_cond)
			count_query = count_query.where(filter_cond)

		if params.memory_type:
			query = query.where(MemoryItem.memory_type == params.memory_type.value)
			count_query = count_query.where(MemoryItem.memory_type == params.memory_type.value)

		if params.date_from:
			query = query.where(MemoryItem.timestamp >= params.date_from)
			count_query = count_query.where(MemoryItem.timestamp >= params.date_from)

		if params.date_to:
			query = query.where(MemoryItem.timestamp <= params.date_to)
			count_query = count_query.where(MemoryItem.timestamp <= params.date_to)

		if params.confidence:
			query = query.where(MemoryItem.confidence_level == params.confidence.value)
			count_query = count_query.where(MemoryItem.confidence_level == params.confidence.value)

		if params.approval_state:
			query = query.where(MemoryItem.approval_state == params.approval_state.value)
			count_query = count_query.where(MemoryItem.approval_state == params.approval_state.value)

		if params.status:
			query = query.where(MemoryItem.status == params.status.value)
			count_query = count_query.where(MemoryItem.status == params.status.value)

		if params.project_id:
			json_filter = MemoryItem.metadata_json["project_id"].astext == str(params.project_id)
			query = query.where(json_filter)
			count_query = count_query.where(json_filter)

		if params.person:
			json_filter = MemoryItem.metadata_json["person"].astext == params.person
			query = query.where(json_filter)
			count_query = count_query.where(json_filter)

		if params.identity_theme:
			json_filter = MemoryItem.metadata_json["identity_theme"].astext == params.identity_theme
			query = query.where(json_filter)
			count_query = count_query.where(json_filter)

		total_result = await session.execute(count_query)
		total = total_result.scalar_one()

		query = query.order_by(MemoryItem.timestamp.desc()).limit(params.limit).offset(params.offset)
		result = await session.execute(query)
		items = list(result.scalars().all())

		return items, total

	@staticmethod
	async def update(
		session: AsyncSession,
		memory_id: uuid.UUID,
		data: MemoryItemUpdate,
		actor: CurrentUser,
	) -> MemoryItem:
		"""Update a memory item and log an audit event."""
		item = await MemoryService.get(session, memory_id)
		before = _memory_to_dict(item)

		update_data = data.model_dump(exclude_unset=True)
		for field, value in update_data.items():
			if field == "metadata":
				item.metadata_json = value or {}
			elif field == "memory_type" and value is not None:
				item.memory_type = value.value if hasattr(value, "value") else value
			elif field == "confidence_level" and value is not None:
				item.confidence_level = value.value if hasattr(value, "value") else value
			elif field == "status" and value is not None:
				item.status = value.value if hasattr(value, "value") else value
			elif field == "approval_state" and value is not None:
				item.approval_state = value.value if hasattr(value, "value") else value
			else:
				setattr(item, field, value)

		item.updated_by = actor.user_id
		await session.flush()

		await AuditService.log_event(
			session,
			event_type="memory_updated",
			entity_type="memory_item",
			entity_id=item.id,
			actor=actor.user_id,
			actor_type=actor.role.value,
			before_json=before,
			after_json=_memory_to_dict(item),
		)

		await session.refresh(item, attribute_names=["outgoing_links", "incoming_links"])

		return item

	@staticmethod
	async def reinforce(
		session: AsyncSession,
		memory_id: uuid.UUID,
		reason: str,
		source_ref: str | None,
		actor: CurrentUser,
	) -> MemoryItem:
		"""Reinforce a memory: bump salience_score and create a 'supports' self-link."""
		item = await MemoryService.get(session, memory_id)
		before = _memory_to_dict(item)

		current_salience = item.salience_score or Decimal("0.00")
		item.salience_score = min(current_salience + Decimal("1.00"), Decimal("999.99"))
		item.updated_by = actor.user_id
		await session.flush()

		link = MemoryLink(
			id=uuid.uuid4(),
			from_memory_id=memory_id,
			to_memory_id=memory_id,
			link_type="supports",
			weight=Decimal("1.00"),
		)
		session.add(link)
		await session.flush()

		await AuditService.log_event(
			session,
			event_type="memory_reinforced",
			entity_type="memory_item",
			entity_id=memory_id,
			actor=actor.user_id,
			actor_type=actor.role.value,
			before_json=before,
			after_json=_memory_to_dict(item),
			rationale=reason,
			provenance_json={"source_ref": source_ref} if source_ref else None,
		)

		await session.refresh(item, attribute_names=["outgoing_links", "incoming_links"])

		return item

	@staticmethod
	async def contradict(
		session: AsyncSession,
		memory_id: uuid.UUID,
		contradicting_id: uuid.UUID,
		reason: str,
		actor: CurrentUser,
	) -> MemoryLink:
		"""Record a contradiction between two memories."""
		await MemoryService.get(session, memory_id)
		await MemoryService.get(session, contradicting_id)

		link = MemoryLink(
			id=uuid.uuid4(),
			from_memory_id=contradicting_id,
			to_memory_id=memory_id,
			link_type="contradicts",
			weight=Decimal("1.00"),
		)
		session.add(link)
		await session.flush()

		await AuditService.log_event(
			session,
			event_type="memory_contradicted",
			entity_type="memory_item",
			entity_id=memory_id,
			actor=actor.user_id,
			actor_type=actor.role.value,
			after_json={
				"link_id": str(link.id),
				"from_memory_id": str(contradicting_id),
				"to_memory_id": str(memory_id),
				"link_type": "contradicts",
			},
			rationale=reason,
		)

		return link

	@staticmethod
	async def create_link(
		session: AsyncSession,
		data: MemoryLinkCreate,
		actor: CurrentUser,
	) -> MemoryLink:
		"""Create a link between two memory items."""
		await MemoryService.get(session, data.from_memory_id)
		await MemoryService.get(session, data.to_memory_id)

		link = MemoryLink(
			id=uuid.uuid4(),
			from_memory_id=data.from_memory_id,
			to_memory_id=data.to_memory_id,
			link_type=data.link_type,
			weight=data.weight or Decimal("1.00"),
		)
		session.add(link)
		await session.flush()

		await AuditService.log_event(
			session,
			event_type="memory_linked",
			entity_type="memory_link",
			entity_id=link.id,
			actor=actor.user_id,
			actor_type=actor.role.value,
			after_json={
				"link_id": str(link.id),
				"from_memory_id": str(data.from_memory_id),
				"to_memory_id": str(data.to_memory_id),
				"link_type": data.link_type,
			},
		)

		return link
