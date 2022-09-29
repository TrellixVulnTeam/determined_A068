CREATE TABLE experiment_groups (
  id SERIAL PRIMARY KEY,
  name TEXT NOT NULL,
  project_id INT REFERENCES projects(id),
  UNIQUE (name, project_id)
);

ALTER TABLE experiments ADD COLUMN group_id INT REFERENCES experiment_groups(id) NULL;

-- Indexes
CREATE INDEX ix_experiment_groups_id ON public.experiment_groups USING btree (project_id);