"""
becomussy – Thread service.

Provides CRUD and listing for threads.
"""

from __future__ import annotations

import uuid

from fastapi import HTTPException, status
from sqlalchemy import func, select
from sqlalchemy.ext.asyncio import AsyncSession

from app.core.security import CurrentUser
from app.models.thread import Thread
from app.schemas.thread import ThreadCreate, ThreadSearchParams, ThreadUpdate
from app.services.audit import AuditService


def _thread_to_dict(t: Thread) -> dict:
	"""Convert a Thread to a JSON-serialisable dict for audit logging."""
	return {
		"id": str(t.id),
		"title": t.title,
		"description": t.description,
		"thread_type": t.thread_type,
		"status": t.status,
		"urgency": t.urgency,
		"importance": t.importance,
		"next_action": t.next_action,
		"blocker": t.blocker,
		"steward_visibility": t.steward_visibility,
	}


class ThreadService:
	"""Service layer for thread operations."""

	@staticmethod
	async def create(
		session: AsyncSession,
		data: ThreadCreate,
		actor: CurrentUser,
	) -> Thread:
		"""Create a new thread and log an audit event."""
		thread = Thread(
			id=uuid.uuid4(),
			title=data.title,
			description=data.description,
			thread_type=data.thread_type,
			status=data.status.value if data.status else "active",
			urgency=data.urgency,
			importance=data.importance,
			next_action=data.next_action,
			blocker=data.blocker,
			steward_visibility=data.steward_visibility,
			metadata_json=data.metadata_json or {},
			updated_by=actor.user_id,
		)
		session.add(thread)
		await session.flush()

		await AuditService.log_event(
			session,
			event_type="thread_created",
			entity_type="thread",
			entity_id=thread.id,
			actor=actor.user_id,
			actor_type=actor.role.value,
			after_json=_thread_to_dict(thread),
		)

		return thread

	@staticmethod
	async def get(session: AsyncSession, thread_id: uuid.UUID) -> Thread:
		"""Retrieve a thread by ID, or raise 404."""
		result = await session.execute(
			select(Thread).where(Thread.id == thread_id)
		)
		thread = result.scalar_one_or_none()
		if thread is None:
			raise HTTPException(
				status_code=status.HTTP_404_NOT_FOUND,
				detail=f"Thread {thread_id} not found",
			)
		return thread

	@staticmethod
	async def list(
		session: AsyncSession,
		params: ThreadSearchParams,
	) -> tuple[list[Thread], int]:
		"""List threads with optional filters and pagination. Returns (items, total)."""
		query = select(Thread)
		count_query = select(func.count()).select_from(Thread)

		if params.status:
			query = query.where(Thread.status == params.status.value)
			count_query = count_query.where(Thread.status == params.status.value)

		if params.thread_type:
			query = query.where(Thread.thread_type == params.thread_type)
			count_query = count_query.where(Thread.thread_type == params.thread_type)

		total_result = await session.execute(count_query)
		total = total_result.scalar_one()

		query = query.order_by(Thread.updated_at.desc()).limit(params.limit).offset(params.offset)
		result = await session.execute(query)
		items = list(result.scalars().all())

		return items, total

	@staticmethod
	async def update(
		session: AsyncSession,
		thread_id: uuid.UUID,
		data: ThreadUpdate,
		actor: CurrentUser,
	) -> Thread:
		"""Update a thread and log an audit event."""
		thread = await ThreadService.get(session, thread_id)
		before = _thread_to_dict(thread)

		update_data = data.model_dump(exclude_unset=True)
		for field, value in update_data.items():
			if field == "status" and value is not None:
				thread.status = value.value if hasattr(value, "value") else value
			else:
				setattr(thread, field, value)

		thread.updated_by = actor.user_id
		await session.flush()

		await AuditService.log_event(
			session,
			event_type="thread_updated",
			entity_type="thread",
			entity_id=thread.id,
			actor=actor.user_id,
			actor_type=actor.role.value,
			before_json=before,
			after_json=_thread_to_dict(thread),
		)

		return await ThreadService.get(session, thread.id)
