#ifndef ORISUN_FLUTTER_H
#define ORISUN_FLUTTER_H

#include <stdint.h>

uint32_t orisun_abi_version(void);
char *orisun_open(char *data_directory, char *boundaries_json);
char *orisun_close(uint64_t store_handle);
char *orisun_save_events(uint64_t store_handle, char *boundary,
                         char *events_json, char *expected_position_json,
                         char *query_json);
char *orisun_get_events(uint64_t store_handle, char *boundary,
                        char *from_position_json, char *query_json,
                        int64_t count, int32_t descending);
char *orisun_get_latest_by_criteria(uint64_t store_handle, char *boundary,
                                    char *query_json);
char *orisun_create_boundary_index(uint64_t store_handle, char *boundary,
                                   char *name, char *fields_json,
                                   char *conditions_json, char *combinator);
char *orisun_drop_boundary_index(uint64_t store_handle, char *boundary,
                                 char *name);
char *orisun_subscribe(uint64_t store_handle, char *boundary,
                       char *subscriber_name, char *after_position_json,
                       char *query_json);
char *orisun_subscription_next(uint64_t subscription_handle,
                               int64_t timeout_milliseconds);
char *orisun_subscription_stop(uint64_t subscription_handle);
void orisun_free_string(char *value);

#endif
