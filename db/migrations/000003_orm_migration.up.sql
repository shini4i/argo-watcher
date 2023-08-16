ALTER TABLE public.tasks ALTER COLUMN "id" TYPE uuid using id::uuid;
ALTER TABLE public.tasks ALTER COLUMN "id" SET default gen_random_uuid();

UPDATE public.tasks SET updated = NOW() WHERE updated IS NULL;
UPDATE public.tasks SET app = '' WHERE app IS NULL;
UPDATE public.tasks SET author = '' WHERE author IS NULL;
UPDATE public.tasks SET project = '' WHERE project IS NULL;
UPDATE public.tasks SET status_reason = '' WHERE status_reason IS NULL;

ALTER TABLE public.tasks ALTER COLUMN updated SET NOT NULL;
ALTER TABLE public.tasks ALTER COLUMN app SET NOT NULL;
ALTER TABLE public.tasks ALTER COLUMN app DROP DEFAULT;
ALTER TABLE public.tasks ALTER COLUMN author SET NOT NULL;
ALTER TABLE public.tasks ALTER COLUMN author DROP DEFAULT;
ALTER TABLE public.tasks ALTER COLUMN project SET NOT NULL;
ALTER TABLE public.tasks ALTER COLUMN project DROP DEFAULT;
ALTER TABLE public.tasks ALTER COLUMN status_reason DROP DEFAULT;
ALTER TABLE public.tasks ALTER COLUMN images TYPE JSONB;

CREATE INDEX IF NOT EXISTS "idx_tasks_status" ON "tasks" ("status");