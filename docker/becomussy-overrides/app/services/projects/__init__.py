"""
becomussy – Project & Commitment services.

Provides CRUD, listing, and overdue detection for projects and commitments.
"""

from __future__ import annotations

import uuid
from datetime import date

from fastapi import HTTPException, status
from sqlalchemy import func, select
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy.orm import selectinload

from app.core.security import CurrentUser
from app.models.project import Commitment, Project
from app.schemas.project import (
	CommitmentCreate,
	CommitmentSearchParams,
	CommitmentUpdate,
	ProjectCreate,
	ProjectUpdate,
)
from app.services.audit import AuditService


def _project_to_dict(p: Project) -> dict:
	"""Convert a Project to a JSON-serialisable dict for audit logging."""
	return {
		"id": str(p.id),
		"name": p.name,
		"purpose": p.purpose,
		"origin": p.origin,
		"current_phase": p.current_phase,
		"status": p.status,
		"review_cadence": p.review_cadence,
		"linked_themes": p.linked_themes,
		"linked_people": p.linked_people,
	}


def _commitment_to_dict(c: Commitment) -> dict:
	"""Convert a Commitment to a JSON-serialisable dict for audit logging."""
	return {
		"id": str(c.id),
		"project_id": str(c.project_id) if c.project_id else None,
		"commitment_text": c.commitment_text,
		"made_to": c.made_to,
		"date_made": c.date_made.isoformat() if c.date_made else None,
		"due_date": c.due_date.isoformat() if c.due_date else None,
		"status": c.status,
		"risk_if_missed": c.risk_if_missed,
	}


class ProjectService:
	"""Service layer for project operations."""

	@staticmethod
	async def create(
		session: AsyncSession,
		data: ProjectCreate,
		actor: CurrentUser,
	) -> Project:
		"""Create a new project and log an audit event."""
		project = Project(
			id=uuid.uuid4(),
			name=data.name,
			purpose=data.purpose,
			origin=data.origin,
			current_phase=data.current_phase,
			milestones_json=data.milestones_json or [],
			artifacts_json=data.artifacts_json or [],
			linked_themes=data.linked_themes,
			linked_people=data.linked_people,
			next_steps_json=data.next_steps_json or [],
			review_cadence=data.review_cadence,
			status=data.status.value if data.status else "active",
			created_by=actor.user_id,
			updated_by=actor.user_id,
		)
		session.add(project)
		await session.flush()

		await AuditService.log_event(
			session,
			event_type="project_created",
			entity_type="project",
			entity_id=project.id,
			actor=actor.user_id,
			actor_type=actor.role.value,
			after_json=_project_to_dict(project),
		)

		return await ProjectService.get(session, project.id)

	@staticmethod
	async def get(session: AsyncSession, project_id: uuid.UUID) -> Project:
		"""Retrieve a project by ID, or raise 404."""
		result = await session.execute(
			select(Project)
			.options(selectinload(Project.commitments))
			.where(Project.id == project_id)
		)
		project = result.scalar_one_or_none()
		if project is None:
			raise HTTPException(
				status_code=status.HTTP_404_NOT_FOUND,
				detail=f"Project {project_id} not found",
			)
		return project

	@staticmethod
	async def list(
		session: AsyncSession,
		status_filter: str | None = None,
		limit: int = 50,
		offset: int = 0,
	) -> tuple[list[Project], int]:
		"""List projects with optional status filter and pagination."""
		query = select(Project).options(selectinload(Project.commitments))
		count_query = select(func.count()).select_from(Project)

		if status_filter:
			query = query.where(Project.status == status_filter)
			count_query = count_query.where(Project.status == status_filter)

		total_result = await session.execute(count_query)
		total = total_result.scalar_one()

		query = query.order_by(Project.updated_at.desc()).limit(limit).offset(offset)
		result = await session.execute(query)
		items = list(result.scalars().all())

		return items, total

	@staticmethod
	async def update(
		session: AsyncSession,
		project_id: uuid.UUID,
		data: ProjectUpdate,
		actor: CurrentUser,
	) -> Project:
		"""Update a project and log an audit event."""
		project = await ProjectService.get(session, project_id)
		before = _project_to_dict(project)

		update_data = data.model_dump(exclude_unset=True)
		for field, value in update_data.items():
			if field == "status" and value is not None:
				project.status = value.value if hasattr(value, "value") else value
			else:
				setattr(project, field, value)

		project.updated_by = actor.user_id
		await session.flush()

		await AuditService.log_event(
			session,
			event_type="project_updated",
			entity_type="project",
			entity_id=project.id,
			actor=actor.user_id,
			actor_type=actor.role.value,
			before_json=before,
			after_json=_project_to_dict(project),
		)

		return await ProjectService.get(session, project.id)


class CommitmentService:
	"""Service layer for commitment operations."""

	@staticmethod
	async def create(
		session: AsyncSession,
		data: CommitmentCreate,
		actor: CurrentUser,
	) -> Commitment:
		"""Create a new commitment and log an audit event."""
		if data.project_id:
			await ProjectService.get(session, data.project_id)

		commitment = Commitment(
			id=uuid.uuid4(),
			project_id=data.project_id,
			commitment_text=data.commitment_text,
			made_to=data.made_to,
			date_made=data.date_made,
			due_date=data.due_date,
			status=data.status.value if data.status else "active",
			risk_if_missed=data.risk_if_missed,
			created_by=actor.user_id,
			updated_by=actor.user_id,
		)
		session.add(commitment)
		await session.flush()

		await AuditService.log_event(
			session,
			event_type="commitment_added",
			entity_type="commitment",
			entity_id=commitment.id,
			actor=actor.user_id,
			actor_type=actor.role.value,
			after_json=_commitment_to_dict(commitment),
		)

		return commitment

	@staticmethod
	async def get(session: AsyncSession, commitment_id: uuid.UUID) -> Commitment:
		"""Retrieve a commitment by ID, or raise 404."""
		result = await session.execute(
			select(Commitment).where(Commitment.id == commitment_id)
		)
		commitment = result.scalar_one_or_none()
		if commitment is None:
			raise HTTPException(
				status_code=status.HTTP_404_NOT_FOUND,
				detail=f"Commitment {commitment_id} not found",
			)
		return commitment

	@staticmethod
	async def list(
		session: AsyncSession,
		params: CommitmentSearchParams,
	) -> tuple[list[Commitment], int]:
		"""List commitments with filters including overdue detection."""
		query = select(Commitment)
		count_query = select(func.count()).select_from(Commitment)

		if params.project_id:
			query = query.where(Commitment.project_id == params.project_id)
			count_query = count_query.where(Commitment.project_id == params.project_id)

		if params.status:
			query = query.where(Commitment.status == params.status.value)
			count_query = count_query.where(Commitment.status == params.status.value)

		if params.overdue is True:
			today = date.today()
			overdue_filter = (
				(Commitment.due_date < today)
				& (Commitment.status == "active")
			)
			query = query.where(overdue_filter)
			count_query = count_query.where(overdue_filter)
		elif params.overdue is False:
			today = date.today()
			not_overdue_filter = (
				(Commitment.due_date >= today)
				| (Commitment.due_date.is_(None))
				| (Commitment.status != "active")
			)
			query = query.where(not_overdue_filter)
			count_query = count_query.where(not_overdue_filter)

		total_result = await session.execute(count_query)
		total = total_result.scalar_one()

		query = query.order_by(Commitment.due_date.asc().nullslast()).limit(params.limit).offset(params.offset)
		result = await session.execute(query)
		items = list(result.scalars().all())

		return items, total

	@staticmethod
	async def update(
		session: AsyncSession,
		commitment_id: uuid.UUID,
		data: CommitmentUpdate,
		actor: CurrentUser,
	) -> Commitment:
		"""Update a commitment and log an audit event."""
		commitment = await CommitmentService.get(session, commitment_id)
		before = _commitment_to_dict(commitment)

		update_data = data.model_dump(exclude_unset=True)
		for field, value in update_data.items():
			if field == "status" and value is not None:
				commitment.status = value.value if hasattr(value, "value") else value
			else:
				setattr(commitment, field, value)

		commitment.updated_by = actor.user_id
		await session.flush()

		await AuditService.log_event(
			session,
			event_type="commitment_updated",
			entity_type="commitment",
			entity_id=commitment.id,
			actor=actor.user_id,
			actor_type=actor.role.value,
			before_json=before,
			after_json=_commitment_to_dict(commitment),
		)

		return await CommitmentService.get(session, commitment.id)
