-- +goose Up
-- Историческая выборка для чтения скидки, без создания аренд и событий.
insert into public.trips (
    rental_id, user_id, scooter_id, plan, rate_minor, advance_minor,
    charged_minor, total_minor, adjustment_minor, billed_minutes, status,
    rental_version, reserved_at, started_at, finished_at, version
)
select md5('discount-trip-' || user_id::text || '-' || n::text)::uuid,
       user_id,
       '3499a1fb-030c-47a8-bcd2-0ce8304d6a22'::uuid,
       'BASE', 1000, 5000, 1000, 1000, -4000, 1, 'COMPLETED', 3,
       '2026-01-01T00:00:00Z'::timestamptz,
       '2026-01-01T00:00:01Z'::timestamptz,
       '2026-01-01T00:01:01Z'::timestamptz, 3
from (values
    ('10000000-0000-4000-8000-000000000001'::uuid, 9),
    ('10000000-0000-4000-8000-000000000002'::uuid, 9)
) as users(user_id, trips)
cross join lateral generate_series(1, trips) as n
on conflict do nothing;

-- +goose Down
delete from public.trips
where rental_id in (
    select md5('discount-trip-' || user_id::text || '-' || n::text)::uuid
    from (values
        ('10000000-0000-4000-8000-000000000001'::uuid, 9),
        ('10000000-0000-4000-8000-000000000002'::uuid, 9)
    ) as users(user_id, trips)
    cross join lateral generate_series(1, trips) as n
);
