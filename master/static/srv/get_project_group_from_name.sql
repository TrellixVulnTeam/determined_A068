SELECT g.id, g.name, g.project_id
FROM project_experiment_groups g
WHERE g.project_id = $1 AND g.name = $2;
