-- +goose Up
-- Тысяча известных UUID для одинаковой нагрузки до и после изменений.
insert into public.users (id, name, email, version, updated_at)
select ('10000000-0000-4000-8000-' || lpad(n::text, 12, '0'))::uuid,
       'Модуль 10, пользователь ' || n, 'module10-' || n || '@example.test', 1, now()
from generate_series(1, 1000) as n
on conflict do nothing;

insert into public.subscriptions (user_id, plan, updated_at)
select ('10000000-0000-4000-8000-' || lpad(n::text, 12, '0'))::uuid, 'BASE', now()
from generate_series(1, 1000) as n
on conflict do nothing;

-- +goose Down
delete from public.subscriptions where user_id in (
    select ('10000000-0000-4000-8000-' || lpad(n::text, 12, '0'))::uuid
    from generate_series(1, 1000) as n
);
delete from public.users where id in (
    select ('10000000-0000-4000-8000-' || lpad(n::text, 12, '0'))::uuid
    from generate_series(1, 1000) as n
);
