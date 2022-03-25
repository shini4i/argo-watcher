-- migrate:up
ALTER TABLE public.tasks
ADD COLUMN project VARCHAR(255) DEFAULT NULL;

-- migrate:down

